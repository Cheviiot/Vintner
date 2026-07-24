// toolrelay.exe - a thin native launcher for a Wine-hosted MSVC tool
// invocation, built and used by msvc-go-wine (see internal/install's
// buildToolRelay and internal/wrapper's runViaToolRelay).
//
// It exists to solve two problems that are only solvable from inside a
// real Windows process, not from the Unix side of Wine:
//
//   1. Wine truncates a Windows process's exit code down to a single byte
//      when reporting it back to the host OS. mt.exe (the manifest tool)
//      can legitimately exit with 0x41020001 ("manifest unchanged"), which
//      CMake's own build driver specifically checks for and translates to
//      0xbb before treating it as success - see cmcmd.cxx upstream. If
//      Wine has already truncated 0x41020001 down to 0x01 by the time our
//      Go wrapper observes it, we can no longer make that translation
//      ourselves. Only a native Windows process calling
//      GetExitCodeProcess() can see the real, untruncated value - so this
//      helper does the translation itself, then exits with the already-
//      small 0xbb, which fits in a byte and survives Wine's truncation
//      intact.
//
//   2. It gives the launched process its own Job Object (with
//      JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE), so an aborted build doesn't
//      leave orphaned child processes running under Wine.
//
// Usage: toolrelay.exe <real-tool.exe> [args...]
//
// Optional environment variables MSVCGOWINE_STDIN / MSVCGOWINE_STDOUT /
// MSVCGOWINE_STDERR name files to redirect the child's standard handles to
// (used by runViaToolRelay to relay output through named pipes back to the
// Unix side); any that aren't set fall back to this process's own
// inherited handles.

#include <windows.h>
#include <string>

namespace {

constexpr DWORD kMtManifestUnchanged = 0x41020001;
constexpr DWORD kMtManifestUnchangedForCMake = 0xbb;

HANDLE OpenRedirectOrFallback(const wchar_t *envVar, DWORD access,
                              DWORD disposition, HANDLE fallback) {
  wchar_t path[32768];
  DWORD n = GetEnvironmentVariableW(envVar, path, ARRAYSIZE(path));
  if (n == 0 || n >= ARRAYSIZE(path)) {
    return fallback;
  }
  SECURITY_ATTRIBUTES sa{sizeof(sa), nullptr, TRUE};
  HANDLE h = CreateFileW(path, access,
                         FILE_SHARE_READ | FILE_SHARE_WRITE | FILE_SHARE_DELETE,
                         &sa, disposition, FILE_ATTRIBUTE_NORMAL, nullptr);
  return h == INVALID_HANDLE_VALUE ? fallback : h;
}

// CreateProcessW wants a single mutable command line string, quoting each
// argument and escaping embedded quotes.
std::wstring BuildCommandLine(int argc, wchar_t **argv) {
  std::wstring cmd;
  for (int i = 0; i < argc; i++) {
    if (i > 0) cmd += L' ';
    cmd += L'"';
    for (const wchar_t *c = argv[i]; *c; c++) {
      if (*c == L'"') cmd += L'\\';
      cmd += *c;
    }
    cmd += L'"';
  }
  return cmd;
}

HANDLE MakeKillOnCloseJob() {
  HANDLE job = CreateJobObjectW(nullptr, nullptr);
  if (!job) return nullptr;
  JOBOBJECT_EXTENDED_LIMIT_INFORMATION info{};
  info.BasicLimitInformation.LimitFlags =
      JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE | JOB_OBJECT_LIMIT_SILENT_BREAKAWAY_OK;
  SetInformationJobObject(job, JobObjectExtendedLimitInformation, &info, sizeof(info));
  return job;
}

bool IsMtExe(const wchar_t *path) {
  const wchar_t *name = wcsrchr(path, L'\\');
  name = name ? name + 1 : path;
  return _wcsicmp(name, L"mt.exe") == 0;
}

}  // namespace

int wmain(int argc, wchar_t **argv) {
  if (argc < 2) {
    return 1;
  }
  const wchar_t *targetExe = argv[1];

  STARTUPINFOW si{sizeof(si)};
  si.dwFlags = STARTF_USESTDHANDLES;
  si.hStdInput = OpenRedirectOrFallback(L"MSVCGOWINE_STDIN", GENERIC_READ,
                                        OPEN_EXISTING, GetStdHandle(STD_INPUT_HANDLE));
  si.hStdOutput = OpenRedirectOrFallback(L"MSVCGOWINE_STDOUT", GENERIC_WRITE,
                                         CREATE_ALWAYS, GetStdHandle(STD_OUTPUT_HANDLE));
  si.hStdError = OpenRedirectOrFallback(L"MSVCGOWINE_STDERR", GENERIC_WRITE,
                                        CREATE_ALWAYS, GetStdHandle(STD_ERROR_HANDLE));

  HANDLE job = MakeKillOnCloseJob();
  std::wstring cmdLine = BuildCommandLine(argc - 1, argv + 1);

  PROCESS_INFORMATION pi{};
  DWORD exitCode;
  if (CreateProcessW(targetExe, cmdLine.empty() ? nullptr : &cmdLine[0], nullptr,
                      nullptr, TRUE, 0, nullptr, nullptr, &si, &pi)) {
    if (job) AssignProcessToJobObject(job, pi.hProcess);
    WaitForSingleObject(pi.hProcess, INFINITE);
    if (!GetExitCodeProcess(pi.hProcess, &exitCode)) {
      exitCode = GetLastError();
    }
    CloseHandle(pi.hThread);
    CloseHandle(pi.hProcess);
  } else {
    exitCode = GetLastError();
  }

  if (IsMtExe(targetExe) && exitCode == kMtManifestUnchanged) {
    exitCode = kMtManifestUnchangedForCMake;
  }
  return static_cast<int>(exitCode);
}
