# release-build-artifacts Specification

## Purpose
Defines the cross-platform release artifact build process, ensuring the pgcqrs project produces correctly compiled and packaged binaries for Linux and macOS platforms, suitable for distribution via GitHub Releases.

## Requirements

### Requirement: Linux release artifacts include amd64 and arm64 architectures
The release build process SHALL produce tarballs for Linux targeting both amd64 and arm64 architectures. Each tarball SHALL contain all three binaries: service, pgcqrs, and migrator.

#### Scenario: Linux amd64 tarball contains all binaries
- **WHEN** the release build process runs for Linux amd64
- **THEN** the build produces `pgcqrs_amd64_linux.tgz` containing `service`, `pgcqrs`, and `migrator` binaries

#### Scenario: Linux arm64 tarball contains all binaries
- **WHEN** the release build process runs for Linux arm64
- **THEN** the build produces `pgcqrs_arm64_linux.tgz` containing `service`, `pgcqrs`, and `migrator` binaries

### Requirement: macOS release artifacts target arm64 architecture only
The release build process SHALL produce a tarball for macOS (darwin) targeting the arm64 architecture only. The tarball SHALL contain all three binaries: service, pgcqrs, and migrator.

#### Scenario: macOS arm64 tarball contains all binaries
- **WHEN** the release build process runs for macOS arm64
- **THEN** the build produces `pgcqrs_arm64_darwin.tgz` containing `service`, `pgcqrs`, and `migrator` binaries

#### Scenario: macOS binaries are Mach-O arm64 executables
- **WHEN** the release build process completes for macOS arm64
- **THEN** the binaries in the tarball are Mach-O 64-bit arm64 executables

### Requirement: Release artifacts are statically linked
The release build process SHALL produce statically linked binaries with `CGO_ENABLED=0`, ensuring portability across different Linux distributions and macOS versions without external library dependencies.

#### Scenario: Linux binaries have no dynamic dependencies
- **WHEN** the release build process completes for Linux
- **THEN** the binaries are statically linked and can run without requiring specific glibc versions or other shared libraries

#### Scenario: macOS binaries are self-contained
- **WHEN** the release build process completes for macOS
- **THEN** the binaries are statically linked and can run on macOS systems without external dependencies

### Requirement: CI workflow builds all platform artifacts
The CI release workflow SHALL invoke the release build process for both Linux (amd64 and arm64) and macOS (arm64) platforms, producing all expected tarballs for upload to GitHub Releases.

#### Scenario: CI produces Linux amd64 and arm64 tarballs
- **WHEN** the CI release workflow executes the Linux build step
- **THEN** the workflow produces `pgcqrs_amd64_linux.tgz` and `pgcqrs_arm64_linux.tgz`

#### Scenario: CI produces macOS arm64 tarball
- **WHEN** the CI release workflow executes the macOS build step
- **THEN** the workflow produces `pgcqrs_arm64_darwin.tgz`

#### Scenario: All tarballs are uploaded to GitHub Release
- **WHEN** the CI release workflow completes
- **THEN** all produced tarballs (Linux amd64, Linux arm64, macOS arm64) are uploaded to the GitHub Release

### Requirement: Release build respects configurable targets
The release build script SHALL accept environment variables to override the default build targets, allowing selective builds for specific platforms or architectures during development and testing.

#### Scenario: Override TARGET_OS to build single platform
- **WHEN** `TARGET_OS` is set to `linux`
- **THEN** the release build produces only Linux tarballs (both architectures by default)

#### Scenario: Override TARGET_ARCHS to build single architecture
- **WHEN** `TARGET_ARCHS` is set to `arm64`
- **THEN** the release build produces only arm64 tarballs for the target OS

#### Scenario: Default build produces all platforms
- **WHEN** no environment variables are set
- **THEN** the release build produces tarballs for both Linux (amd64, arm64) and macOS (arm64)

### Requirement: Artifact naming follows consistent convention
Release artifacts SHALL follow the naming convention `pgcqrs_<arch>_<os>.tgz`, where `<arch>` is the architecture (amd64 or arm64) and `<os>` is the operating system (linux or darwin).

#### Scenario: Artifact filename matches convention
- **WHEN** the release build produces a tarball
- **THEN** the filename follows the pattern `pgcqrs_<arch>_<os>.tgz`

#### Scenario: Upload glob matches all artifacts
- **WHEN** the CI workflow uploads artifacts using the glob pattern `build/pgcqrs*.tgz`
- **THEN** all produced tarballs (Linux and macOS) are matched and uploaded
