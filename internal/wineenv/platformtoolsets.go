package wineenv

// KnownPlatformToolsets are every numeric PlatformToolset short name
// Microsoft.Cpp.Default.props has ever defined a
// _PlatformToolsetShortNameFor_v<N> entry for (VS2013 through the VS2022
// initial release; excludes the _xp/_wp80/_wp81 variants, which aren't
// purely numeric). vintner only ever installs one compiler generation, but
// real .vcxproj files in the wild are pinned to whichever generation they
// were last edited under - v142 (VS2019) for anything not yet retargeted is
// extremely common. Shared between internal/install (which symlinks these
// names onto the one real MSBuild PlatformToolsets directory) and
// internal/wrapper (which mirrors the same names onto VCInstallDir_<N>/
// VCToolsInstallDir_<N> for the older, environment-variable-driven toolset
// redirect chain) - see doc comments there for why both are needed.
var KnownPlatformToolsets = []string{"90", "100", "110", "120", "140", "141", "142", "143"}
