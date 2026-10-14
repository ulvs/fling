# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**Fling** is a native Windows file-sharing tool that integrates into Explorer's right-click context menu. Users right-click a file, select "Fling — Share file", and the file uploads to an S3-compatible store (AWS S3, MinIO, etc.). A presigned download URL is copied to clipboard with a toast notification.

## Tech Stack

- **C++ with C++/WinRT** (not C++/CX - no hat pointers)
- **WinUI 3** (Windows App SDK) for UI
- **aws-sdk-cpp** (via vcpkg) for S3 uploads
- **nlohmann-json** (via vcpkg) for config/history serialization
- **MSIX-packaged** for context menu integration

## Build System

Visual Studio MSBuild solution (not CMake). Created from "Blank App, Packaged (WinUI 3 in Desktop)" C++ template.

```bash
# Dependencies managed via vcpkg manifest mode (vcpkg.json)
# Ensure vcpkg is installed and integrated:
vcpkg integrate install

# Build via Visual Studio or MSBuild:
msbuild Fling.sln /p:Configuration=Release /p:Platform=x64
```

## Architecture

```
┌─────────────────┐    ┌──────────────────────────┐
│  Explorer Shell  │───▶│     WinUI 3 Tray App     │
│  Context Menu    │    │  ┌────────────────────┐  │
└─────────────────┘    │  │   SettingsPage      │  │
                        │  │   HistoryPage       │  │
                        │  │   UploadDialog      │  │
                        │  └────────────────────┘  │
                        └───────────┬──────────────┘
                                    │
                        ┌───────────▼──────────────┐
                        │       Core Library        │
                        │  S3Uploader               │
                        │  ConfigManager (JSON)     │
                        │  HistoryManager (JSON)    │
                        │  ClipboardHelper          │
                        │  ToastHelper              │
                        └───────────┬──────────────┘
                                    │
                        ┌───────────▼──────────────┐
                        │   S3-Compatible Store     │
                        └──────────────────────────┘
```

## Design system — always follow it

The Fling design system lives in `Fling/Design/` (theme, tokens, logos, component specs, reference image). Read `Fling/Design/README.md` before any UI work. It was designed for a send-to-device concept; Fling uploads to S3 and shares a link, so device-specific parts (DevicePicker, "Delivered · verified") don't apply.

- **Resources, never literals.** Colours come from `{ThemeResource Fling…Brush}`, spacing from `FlingSpace1/2/4/6` or the Thickness resources, corners from `FlingRadiusSm/Md/Full`, text from the `Fling…TextStyle` styles. No hex colours, ad-hoc font sizes or corner radii in XAML or C++. If a value is missing, add it to `Fling/Design/Fling.Theme.xaml` and `Fling/Design/tokens.json`.
- **Colour means something.** Surfaces stay neutral. `FlingBrush` (vermilion): the one primary action per view and the active drop zone. `FlingArcBrush` (yellow): progress only, as a fill. `FlingDoneBrush` (green): uploaded successfully only.
- **No error red.** Failure = warning glyph in ink, plain wording ("Couldn't upload. …") and a primary **Try again**.
- **Buttons:** `FlingPrimaryButtonStyle` at most once per view; otherwise `FlingSecondaryButtonStyle` / `FlingQuietButtonStyle`, and the `…Small…` variants in rows and toasts. Labels are verbs in sentence case.
- **Text:** short sentences, verbs first, sentence case, no exclamation marks or emoji. File names exactly as written. Sizes, speeds and times use `FlingMonoTextStyle`.
- **Surfaces:** borders, not shadows, inside the window (`FlingCardStyle`); shadows only for floating things (toasts).
- **Theme and accessibility:** Light, Dark and HighContrast must all work. Every interactive element keyboard-reachable with visible focus. Status never by colour alone: icon + word.
- **Logos:** only the files in `Fling/Design/Logos/`, chosen by background (`FlingLogoSource` picks per theme). Never redraw, recolour, or set "fling" in a font as a stand-in.
- **Components** (`Fling/Controls/`): `TransferRow` (waiting / sending / done / failed / inactive, driven from code via visual states), `DropZone` (drop or click/Enter/Space to pick files), `FlingToast` (in-window message, one at a time, ~4 s, alerts stay). Glyphs are Segoe Fluent Icons via `FontIcon`.
- **C++ sources are compiled with `/utf-8`.** Write non-ASCII (glyphs, `·`, `…`) as `\uXXXX` escapes so private-use icon glyphs stay readable.

## Key Implementation Constraints

- **AWS SDK initialization**: Call `Aws::InitAPI()` once in `App.xaml.cpp` at startup, `Aws::ShutdownAPI()` at exit. Never per-upload.
- **Single-instance**: Use `AppInstance` redirect to prevent multiple processes.
- **S3 compatibility**: All operations must support custom endpoints for MinIO. Use path-style addressing when `usePathStyle` is true.
- **Tray icon**: Uses Win32 `Shell_NotifyIconW` (WinUI 3 has no built-in tray support).
- **Context menu**: Registered via MSIX manifest `Package.appxmanifest`, not registry.
- **Toast notifications**: Use Windows App SDK `AppNotification` API.
- **Large files (>100MB)**: Use `TransferManager` for multipart uploads.

## Data Storage

- Config: `%LOCALAPPDATA%\Fling\config.json`
- History: `%LOCALAPPDATA%\Fling\history.json` (capped at 500 entries)

## Code Style

- C++20 standard
- Use `std::filesystem` for file paths
- Use `winrt::hstring` for WinRT interop, `std::wstring` elsewhere
- Error handling: Return values (e.g., `UploadResult` with success/error) for expected failures; exceptions for programmer errors
- XAML: Use `x:Bind` for compile-time binding where possible
- Keep headers clean with forward declarations; implementations in `.cpp` files

## Project Structure (Target)

```
Fling/
├── Fling.sln
├── vcpkg.json                    # vcpkg manifest
├── Fling/
│   ├── Fling.vcxproj
│   ├── Package.appxmanifest      # MSIX manifest (context menu, toast, identity)
│   ├── App.xaml(.h/.cpp)         # App lifecycle, AWS SDK init, activation handling
│   ├── MainWindow.xaml(.h/.cpp)  # Tray icon, window management
│   ├── Pages/
│   │   ├── SettingsPage.xaml     # S3 configuration UI
│   │   └── HistoryPage.xaml      # Upload history list
│   ├── Dialogs/
│   │   └── UploadProgressDialog.xaml
│   ├── Core/
│   │   ├── S3Uploader.h/.cpp
│   │   ├── ConfigManager.h/.cpp
│   │   ├── HistoryManager.h/.cpp
│   │   ├── ClipboardHelper.h/.cpp
│   │   └── ToastHelper.h/.cpp
│   └── Models/
│       ├── FlingConfig.h
│       └── UploadEntry.h
```

## Implementation Phases

See `fling-project-plan.md` for detailed implementation steps. Key phases:
1. Project setup & vcpkg integration
2. Core library (ConfigManager, S3Uploader, HistoryManager, helpers)
3. Context menu integration via MSIX manifest
4. WinUI 3 tray app (Settings, History, Progress dialog)
5. Polish (multi-file, drag-drop, large file handling, security)
