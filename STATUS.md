# Fling — Current Status (2026-10-14)

Handoff snapshot for continuing development on another machine.

---

## Where we are

Plan phases (from [fling-project-plan.md](fling-project-plan.md)):

| Phase | Status |
|---|---|
| 1 — Project setup & vcpkg | ✅ Done |
| 2 — Core library (ConfigManager, S3Uploader, HistoryManager, helpers) | ✅ Done |
| 3 — Explorer integration | 🟡 MSIX package + Windows 11 "Fling it" context menu (`FlingShellExt.dll`) landed; release signing not done |
| 4 — WinUI 3 app | 🟡 Fling design system applied (theme, logos, icons). Main window: Uploads (drop zone + recent uploads) and Settings; compact SendWindow with one TransferRow per file; tray icon scaffolded but not called |
| 5 — Polish (multi-file, drag-drop, TransferManager, creds) | ⏳ Not started |

**The end-to-end pipeline works today**: configure S3 creds in Settings → click "Share a file..." on the History page → file uploads to MinIO → presigned URL lands on clipboard → toast notification → entry appears in History.

Verified live against MinIO at `https://s3.intebitter.se` / bucket `fling` / user `fling-app`. Policy has `s3:ListBucket`, `s3:PutObject`, `s3:GetObject`. (Missing `s3:AbortMultipartUpload` — only matters for cancelled large uploads, not blocking.)

---

## Build environment (important)

- **VS 18 (2026) MSBuild + `v145` toolset.** `C:\Program Files\Microsoft Visual Studio\18\Professional\MSBuild\Current\Bin\MSBuild.exe`. Do **not** go back to `v143`: VS 18 then redirects the C++ build to VS 2022's `VC\v170` targets, and the dev box's VS 2022 lacks the WindowsXaml and AppxPackage components. That redirect was the root cause of every XAML build hack from the spring (all removed in the 2026-10 upgrade).
- **Packages:** Windows App SDK 2.5.1, CppWinRT 3.0.260818.1, SDK BuildTools 10.0.26100.9169, WIL 1.0.260126.7.
- **Project shape matches the WinUI C++ template:** `ApplicationType=Windows Store`, `ApplicationTypeRevision=10.0`, `AppContainerApplication=false`, `DISABLE_XAML_GENERATED_MAIN` (we have our own single-instance `main.cpp`), `module.g.cpp` listed explicitly, `DependentUpon` on the XAML code-behind/idl items. Because we use `PackageReference` (not packages.config) we also need the `TargetFramework=native` monikers and `ResolveNuGetPackages=false`.
- **vcpkg manifest mode** (`vcpkg.json`, pinned `builtin-baseline`): aws-sdk-cpp[s3,transfer] (no default features), nlohmann-json, gtest. Installed per-clone into `vcpkg_installed\` by the build (`VcpkgEnableManifest=true`, triplet `x64-windows`); headers/libs auto-linked, DLLs deployed by vcpkg's app-local step (which also feeds the MSIX payload). Needs `vcpkg integrate install` once per machine (VS 18's bundled vcpkg or a vcpkg checkout). `FlingShellExt` opts out (`VcpkgEnabled=false`).
- **Defender vs. vcpkg:** a fresh port checkout can fail with `rename_or_delete(...buildtrees\versioning_\...tmp) ... being used by another process` — real-time scanning briefly locks the just-extracted folder. Retrying, or renaming the complete `*.tmp` folder to its final name by hand, gets past it; a Defender exclusion for the vcpkg root avoids it. The .sln builds Fling.Tests after Fling so the two projects' installs don't race on the same checkout.
- **Defender exclusions recommended** for `E:\dev\fling`.

---

## Build / run / test

```
# Build from repo root
"C:\Program Files\Microsoft Visual Studio\18\Professional\MSBuild\Current\Bin\MSBuild.exe" Fling/Fling.vcxproj -restore -p:Configuration=Debug -p:Platform=x64

# Launch (output layout is per-project now)
Fling\x64\Debug\Fling\Fling.exe

# Tests (38 unit tests; MinIO integration tests skip unless `docker compose up -d` is running)
"...\MSBuild.exe" Fling.Tests/Fling.Tests.vcxproj -p:Configuration=Debug -p:Platform=x64
Fling.Tests\x64\Debug\Fling.Tests.exe
```

vcpkg's app-local step copies the needed DLLs next to both exes.

---

## MSIX (packaged) dev loop

Fling is a full MSIX app (`AppxPackage=true`, framework-dependent on `Microsoft.WindowsAppRuntime.2` ≥ 2.5.1). Identity `FlingApp.Fling`, publisher `CN=FlingApp`.

```
# Build the layout (Release: the Debug layout needs the VCLibs *Debug* frameworks, which only VS F5 deploys)
"...\MSBuild.exe" Fling.sln -restore -p:Configuration=Release -p:Platform=x64

# Register the unsigned layout for the current user (needs Developer Mode)
Add-AppxPackage -Register E:\dev\fling\x64\Release\Fling\AppxManifest.xml   # sln build
Add-AppxPackage -Register E:\dev\fling\Fling\x64\Release\Fling\AppxManifest.xml   # project build

# Launch with identity / uninstall
fling.exe            # app execution alias
Get-AppxPackage FlingApp.Fling | Remove-AppxPackage
```

- **`FlingShellExt/`** — in-proc `IExplorerCommand` DLL (WRL, static CRT, no WinAppSDK) registered via `windows.fileExplorerContextMenus` + `com:SurrogateServer`. `Invoke` launches the `fling.exe` alias with the selected paths so Fling runs with package identity; the existing single-instance redirect handles the upload. Packaged into Fling's layout via a `<None DeploymentContent>` item; its OutDir is pinned so .sln and project builds agree.
- **vcpkg DLLs** reach the package via `_FlingPackageVcpkgDlls`, which reads vcpkg's `$(IntDir)vcpkg.applocal.log`. vcpkg's own hand-off (`ReferenceCopyLocalPaths`) only happens when the linker runs, so a no-op rebuild used to produce an .msix without them.
- **`_FlingDropPhantomWebView2Payload`** target: WebView2's winmd reference implies a non-existent `lib\Microsoft.Web.WebView2.Core.dll`, which collides with the real `native_uap` one (APPX1101).
- **AppData is virtualized** for the packaged app: new writes to `%LOCALAPPDATA%\Fling` land under `%LOCALAPPDATA%\Packages\FlingApp.Fling_*\LocalCache`. The old SendTo shortcut feature was removed for this reason (the context menu replaces it).

---

## Key implementation notes

1. **No `App.idl`.** As in the WinUI template, `App` is not a WinRT runtime class; `App.xaml.h` includes the XAML compiler's `App.xaml.g.h`.
2. Page `.xaml.cpp` files must **not** include `*.xaml.g.hpp` — those are produced by markup pass 2 after the main compile and are pulled in by `XamlTypeInfo.g.cpp`.
3. Generated code lives in `Fling\Generated Files\` (git-ignored); `Generated Files\Pages` and `\Dialogs` are on the include path so `X.g.h` can find `X.xaml.g.h`.
4. **Preprocessor**: `NOMINMAX;WIN32_LEAN_AND_MEAN;USE_IMPORT_EXPORT`. Without `NOMINMAX` the Windows `max`/`min` macros break aws-sdk-cpp's TransferManager.h.
5. **Unpackaged toast notifications are broken on WinAppSDK 2.x self-contained:** `AppNotificationManager::Register()` fails loading `Microsoft.WindowsAppRuntime.Insights.Resource.dll`. App catches it and carries on (no toast). The MSIX manifest now declares the toast activator (`85332090-…`), so packaged runs should register normally — not yet verified with a real toast.

6. **Single-instance hand-off:** the redirecting process runs `RedirectActivationToAsync` on a worker thread and pumps with `CoWaitForMultipleObjects` (blocking the STA deadlocks it), and the running instance reads `Windows.ApplicationModel.Activation.ILaunchActivatedEventArgs` (not the XAML type). Both were broken before 2026-10-14, so sharing while Fling was open never worked.
7. **Launch routing:** `App::OnLaunched` / `HandleRedirectedActivation` open a `SendWindow` when the command line has files, otherwise the main window (created lazily, `App::ShowMainWindow(page)`). The process ends when the last window closes, so `SendWindow` hides instead of closing while an upload runs and reports by toast.
9. **Several items / folders:** a lone file uploads as-is; folders are always zipped (`<folder>.zip`); several items become one `flung-YYYY-MM-DD-HHMM.zip` unless Settings has "A separate link for each file" (`zipMultipleFiles` in config.json). Already-compressed formats are stored, not deflated. The temp zip is deleted after the upload attempt.
10. **Two context-menu handlers, one "Fling it" entry:** `FlingShareCommand` (IExplorerCommand, Windows 11 top-level menu) reports `ECS_HIDDEN` outside explorer.exe; `FlingClassicMenu` (IContextMenu, registered via `desktop9:FileExplorerClassicContextMenuHandler`) skips itself inside explorer.exe. Total Commander and other file managers therefore see exactly one entry, whether or not the Windows build merges packaged verbs into classic menus.
11. **Local log (`Core/Log`):** `LocalState\logs\fling.log` in the package (rolls over at 1 MB, keeps one old file). Settings has "Open log folder" and "Copy diagnostic info". Nothing is sent anywhere. Rules: never log keys, links or file names; describe files with `Log::Describe`/`DescribeType`; `Log::Scrub` strips URL query strings and the user profile path from every line. Crash reports for Store installs come from Windows Error Reporting (Partner Center, Health). Opt-in Sentry is the planned next step if needed.
8. **Sources are compiled with `/utf-8`.** Without it MSVC reads UTF-8 files as Windows-1252 and every non-ASCII literal (`·`, `…`, icon glyphs) turns into mojibake.
---

## Code layout

```
Fling/
  App.xaml(.h/.cpp)                App lifecycle, AWS init, redirected-activation handler
  main.cpp                         AppInstance single-instance redirect
  MainWindow.xaml(.h/.cpp/.idl)    NavigationView + tray icon scaffolding
  SendWindow.xaml(.h/.cpp/.idl)    Small always-on-top upload window for file launches (bottom-right, auto-closes on success)
  Controls/                        TransferRow, DropZone, FlingToast (design-system UserControls)
  Core/ArchiveBuilder.cpp          Zips folders / several items (minizip-ng, static) into %TEMP%\Fling\<id>\
  Design/                          Theme, tokens, logos, component specs (see CLAUDE.md "Design system")
  Pages/
    SettingsPage.xaml(.h/.cpp/.idl)  S3 creds + Test connection
    HistoryPage.xaml(.h/.cpp/.idl)   History list + "Share a file..." button
  Dialogs/
  Core/
    ConfigManager.cpp              JSON round-trip for FlingConfig
    HistoryManager.cpp             JSON round-trip for UploadEntry[], 500-entry cap
    S3Uploader.cpp                 PutObject + TransferManager + presigned URL + HeadBucket
    ClipboardHelper.cpp            WinRT Clipboard
    ToastHelper.cpp                AppNotificationBuilder
    UploadCoordinator.cpp          Orchestrates config → upload → clipboard → toast → history
  Models/
    FlingConfig.h                  + to_json / from_json
    UploadEntry.h                  + to_json / from_json
  pch.h/.cpp                       WinRT + AWS + nlohmann + XAML impl headers
```

Tests in `Fling.Tests/` — gtest 1.17 from vcpkg (own `TestMain.cpp`, since `gtest_main` is manual-link); compiles `Core/ConfigManager`, `HistoryManager`, `S3Uploader` directly. `AwsTestEnvironment.cpp` does the process-wide `Aws::InitAPI`. `HistoryManagerTests.AddEntry_EnforcesMaxEntries` takes ~30 s (500 disk saves).

---

## Known TODOs (by priority)

### Next
- Verify end to end with the packaged app: right-click a file → "Fling it" → URL on clipboard + toast + History entry. Check Settings shows the pre-MSIX config (AppData virtualization).
- Release signing: self-signed `CN=FlingApp` is fine for local installs, but GitHub Releases users would have to trust the cert manually. Options: a real code-signing cert (e.g. Azure Trusted Signing) or Store-only distribution (the Store re-signs).

### Phase 4 polish
- `MainWindow::SetupTrayIcon()` exists but is never called from the constructor; wire it up so the tray icon actually appears.
- Cancel button in SendWindow (`S3Uploader::Cancel` exists; `UploadCoordinator::Upload` creates the uploader internally, so it needs a hook).

### Phase 5
- Multi-file sharing: the context menu already passes every selected path; check `UploadCoordinator` copies a combined result sensibly (currently one upload/toast per file).
- Drag-and-drop onto window or tray icon.
- Credential storage via Windows Credential Manager instead of plain-text JSON in `%LOCALAPPDATA%\Fling\config.json`.
- Add `s3:AbortMultipartUpload` to the MinIO policy before testing files >100 MB with mid-flight cancellation.

---

## Git

- Branches: `main`; `upgrade-winappsdk-2` (WinAppSDK 2.5.1 / CppWinRT 3.0); `msix-packaging` (on top of the upgrade)
- Remote: `https://github.com/ulvs/fling.git`
