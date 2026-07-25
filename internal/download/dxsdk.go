package download

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// The DirectX SDK (June 2010) is the last standalone release of D3DX9 (and
// D3DX10/11, XInput, XAudio2, ...). Microsoft never carried D3DX forward
// into the Windows 10/11 SDK - it's deprecated in favor of D3DCompiler/
// WICTextureLoader/DirectXTex, but plenty of legacy code (this included)
// still links against the real d3dx9.h/d3dx9.lib. Like the WDK (see
// wdk.go), it isn't part of the VS installer manifest or any package feed
// vsman knows about, so this is a separate, self-contained download path
// outside ExpandSelection/FetchPayloads/UnpackSelectedPackages.
//
// The installer is a self-extracting PE with an appended CAB archive.
// cabextract (already a vintner prerequisite - see runMsiExtract's sibling
// extractWindowsSDKPackage) unpacks it directly, without needing Wine or a
// separate archive tool. Its -F/--filter flag (repeatable) restricts
// extraction to the Include and Lib subtrees actually needed for building -
// about 21MB out of the installer's 1.2GB uncompressed payload.

const dxsdkURL = "https://download.microsoft.com/download/A/E/7/AE743F1F-632B-4809-87A9-AA1BB3458E31/DXSDK_Jun10.exe"

// dxsdkSHA256 pins the exact installer build this code was written against
// (verified by fully extracting it with both cabextract and 7z and cross
// checking the file lists) - the June 2010 DirectX SDK is a frozen legacy
// artifact Microsoft is not going to rebuild. A var, not a const, so tests
// can point it at a small fake payload instead of the real 600MB installer.
var dxsdkSHA256 = "705271dc83bfee54d9b94e028426e288d5f070784b7446d164f48ecfbb2a02cb"

// DownloadDXSDK fetches (or reuses a cached copy of) the DirectX SDK (June
// 2010) installer into cacheDir, then unpacks its Include and Lib trees -
// headers and x86/x64 import libs for D3DX9/10/11, XInput, XAudio2, and the
// rest - into destDir/DXSDK. Returns that directory.
func DownloadDXSDK(cacheDir, destDir string) (string, error) {
	if _, err := exec.LookPath("cabextract"); err != nil {
		return "", fmt.Errorf("cabextract not found in PATH (install the cabextract package): %w", err)
	}

	cacheFile := filepath.Join(cacheDir, "DXSDK_Jun10.exe")
	if !isFile(cacheFile) {
		fmt.Println("Downloading DirectX SDK (June 2010)")
		if err := httpDownloadFile(dxsdkURL, cacheFile); err != nil {
			return "", fmt.Errorf("downloading DirectX SDK: %w", err)
		}
	} else {
		fmt.Println("Using existing file", filepath.Base(cacheFile))
	}
	sum, err := sha256File(cacheFile)
	if err != nil {
		return "", err
	}
	if !equalFoldHex(sum, dxsdkSHA256) {
		return "", fmt.Errorf("incorrect hash for downloaded file %s, aborting", filepath.Base(cacheFile))
	}

	scratch, err := os.MkdirTemp(destDir, "dxsdk-unpack-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(scratch)

	if err := runCabextract(cacheFile, scratch, "DXSDK/Include/*", "DXSDK/Lib/*"); err != nil {
		return "", fmt.Errorf("extracting DirectX SDK: %w", err)
	}

	dxsdkDir := filepath.Join(destDir, "DXSDK")
	if err := combineDirTrees(filepath.Join(scratch, "DXSDK"), dxsdkDir); err != nil {
		return "", fmt.Errorf("moving DirectX SDK content into place: %w", err)
	}
	return dxsdkDir, nil
}

// runCabextract extracts srcFile into destDir, restricted to entries
// matching any of patterns (cabextract's -F, repeatable, glob-matched
// against the full in-archive path).
func runCabextract(srcFile, destDir string, patterns ...string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	args := []string{"-q", "-d", destDir}
	for _, p := range patterns {
		args = append(args, "-F", p)
	}
	args = append(args, srcFile)
	cmd := exec.Command("cabextract", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
