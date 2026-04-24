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
