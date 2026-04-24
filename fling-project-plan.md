# Fling — Project Plan & Implementation Guide

## Project Summary

**Fling** is a native Windows file-sharing tool that integrates into the Explorer right-click context menu. The user right-clicks any file, selects "Fling — Share file", and the file is uploaded to an S3-compatible object store (AWS S3, MinIO, or any S3-compatible endpoint). A presigned download URL is generated, copied to the clipboard, and a native Windows toast notification confirms completion. The tool includes a WinUI 3 tray application for settings, upload history, and progress monitoring.

**Tech stack:** C++ (C++/WinRT), WinUI 3 (Windows App SDK), aws-sdk-cpp (via vcpkg), MSIX-packaged.

---

## Architecture Overview

```
┌──────────────────────────────────────────────────────┐
│                    MSIX Package                       │
│                                                       │
│  ┌─────────────────┐    ┌──────────────────────────┐ │
│  │  Explorer Shell  │    │     WinUI 3 Tray App     │ │
│  │  Context Menu    │───▶│                          │ │
│  │  (via manifest)  │    │  ┌────────────────────┐  │ │
│  └─────────────────┘    │  │   Settings Page     │  │ │
│                          │  │  - S3 endpoint      │  │ │
│                          │  │  - Bucket name      │  │ │
│                          │  │  - Credentials      │  │ │
│                          │  │  - Expiry duration  │  │ │
│                          │  │  - Region           │  │ │
│                          │  └────────────────────┘  │ │
│                          │                          │ │
│                          │  ┌────────────────────┐  │ │
│                          │  │   Upload History    │  │ │
│                          │  │  - File name        │  │ │
│                          │  │  - URL (copyable)   │  │ │
│                          │  │  - Upload date      │  │ │
│                          │  │  - Expiry date      │  │ │
│                          │  │  - File size        │  │ │
│                          │  └────────────────────┘  │ │
│                          │                          │ │
│                          │  ┌────────────────────┐  │ │
│                          │  │  Upload Progress    │  │ │
│                          │  │  (popup window)     │  │ │
│                          │  └────────────────────┘  │ │
│                          └──────────────────────────┘ │
│                                      │                │
│                          ┌───────────▼──────────────┐ │
│                          │       Core Library        │ │
│                          │                           │ │
│                          │  S3Uploader (aws-sdk-cpp) │ │
│                          │  ConfigManager (JSON)     │ │
│                          │  ClipboardHelper (WinRT)  │ │
│                          │  ToastHelper (WinRT)      │ │
│                          │  HistoryManager (JSON)    │ │
│                          └───────────────────────────┘ │
│                                      │                │
└──────────────────────────────────────┼────────────────┘
                                       │
                          ┌────────────▼────────────┐
                          │   S3-Compatible Store    │
                          │  (AWS S3 / MinIO / etc)  │
                          └─────────────────────────┘
```

---

## Directory & File Structure

```
Fling/
├── Fling.sln                              # Visual Studio solution
├── vcpkg.json                             # vcpkg manifest (dependencies)
├── vcpkg-configuration.json               # vcpkg baseline
│
├── Fling/                                 # Main WinUI 3 project
│   ├── Fling.vcxproj                      # MSBuild project file
│   ├── Package.appxmanifest               # MSIX manifest (context menu, toast, identity)
│   │
│   ├── App.xaml                           # Application-level resources
│   ├── App.xaml.h / App.xaml.cpp          # App lifecycle, AWS SDK init/shutdown, activation handling
│   │
│   ├── MainWindow.xaml                    # Tray app main window (hidden by default)
│   ├── MainWindow.xaml.h / .cpp           # Window management, tray icon setup
│   │
│   ├── Pages/
│   │   ├── SettingsPage.xaml / .h / .cpp  # S3 configuration UI
│   │   └── HistoryPage.xaml / .h / .cpp   # Upload history list
│   │
│   ├── Dialogs/
│   │   └── UploadProgressDialog.xaml / .h / .cpp  # Upload progress popup
│   │
│   ├── Core/
│   │   ├── S3Uploader.h / .cpp            # S3 upload logic + presigned URL generation
│   │   ├── ConfigManager.h / .cpp         # Read/write JSON config file
│   │   ├── HistoryManager.h / .cpp        # Read/write upload history
│   │   ├── ClipboardHelper.h / .cpp       # Copy text to clipboard
│   │   └── ToastHelper.h / .cpp           # Show native Windows toast notifications
│   │
│   ├── Models/
│   │   ├── FlingConfig.h                  # Config data structure
│   │   └── UploadEntry.h                  # Upload history entry data structure
│   │
│   ├── Assets/
│   │   ├── fling-icon.ico                 # Multi-resolution app icon
│   │   ├── fling-icon-16.png              # Tray icon
│   │   ├── fling-icon-44.png              # App list icon
│   │   ├── fling-icon-150.png             # Medium tile
│   │   └── StoreLogo.png                  # Store logo
│   │
│   └── Strings/
│       └── en-us/
│           └── Resources.resw             # Localized strings
│
└── docs/
    └── README.md
```

---

## Data Models

### FlingConfig

Stored at `%LOCALAPPDATA%\Fling\config.json`.

```json
{
  "endpoint": "https://s3.amazonaws.com",
  "region": "eu-north-1",
  "bucket": "my-fling-bucket",
  "accessKey": "AKIA...",
  "secretKey": "wJalr...",
  "expirySeconds": 604800,
  "usePathStyle": true,
  "keyPrefix": ""
}
```

**Fields:**

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `endpoint` | string | `""` | S3 endpoint URL. Empty = AWS default. Set for MinIO etc. |
| `region` | string | `"us-east-1"` | AWS region. |
| `bucket` | string | `""` | Target bucket name. |
| `accessKey` | string | `""` | AWS access key ID. |
| `secretKey` | string | `""` | AWS secret access key. |
| `expirySeconds` | int | `604800` | Presigned URL expiry (default 7 days). |
| `usePathStyle` | bool | `true` | Use path-style addressing (required for MinIO). |
| `keyPrefix` | string | `""` | Optional prefix for all uploaded keys (e.g., `"shared/"`). |

### UploadEntry

Stored at `%LOCALAPPDATA%\Fling\history.json` as a JSON array.

```json
{
  "id": "a1b2c3d4-...",
  "fileName": "report.pdf",
  "fileSize": 1048576,
  "s3Key": "a1b2c3d4-.../report.pdf",
  "url": "https://...",
  "uploadedAt": "2026-03-25T14:30:00Z",
  "expiresAt": "2026-04-01T14:30:00Z"
}
```

---

## Implementation Phases

### Phase 1: Project Setup & Build Infrastructure

**Goal:** Blank WinUI 3 C++ app that builds, runs, and can include aws-sdk-cpp headers.

**Steps:**

1. **Create the Visual Studio solution.**
   - Open Visual Studio.
   - File → New → Project → search "WinUI" → select **"Blank App, Packaged (WinUI 3 in Desktop)"** for **C++**.
   - Name it `Fling`, put it in the desired directory.
   - Build and run. A blank window should appear.

2. **Set up vcpkg and install aws-sdk-cpp.**
   - Ensure vcpkg is installed and `vcpkg integrate install` has been run (or use vcpkg manifest mode).
   - Create `vcpkg.json` in the solution root:
     ```json
     {
       "name": "fling",
       "version-string": "0.1.0",
       "dependencies": [
         {
           "name": "aws-sdk-cpp",
           "features": ["s3"]
         },
         "nlohmann-json"
       ]
     }
     ```
   - Configure the project to use vcpkg (either via `integrate install` for global MSBuild integration, or by setting the vcpkg toolchain in the project properties).
   - Verify: add `#include <aws/core/Aws.h>` somewhere and confirm it compiles.

3. **Verify nlohmann-json** for config/history serialization.
   - `#include <nlohmann/json.hpp>` should work after vcpkg setup.

4. **Set up the folder structure** inside the project as outlined above (Core/, Models/, Pages/, Dialogs/, Assets/).

---

### Phase 2: Core Library (No UI)

**Goal:** A working S3 upload-and-share pipeline, testable from a simple `main()` or triggered in the `OnLaunched` handler.

#### 2.1 — ConfigManager

Reads/writes `%LOCALAPPDATA%\Fling\config.json` using nlohmann-json.

```cpp
// ConfigManager.h
#pragma once
#include "Models/FlingConfig.h"
#include <filesystem>

class ConfigManager {
public:
    ConfigManager();
    FlingConfig Load();
    void Save(const FlingConfig& config);
    bool IsConfigured() const;
private:
    std::filesystem::path m_configPath;
};
```

**Implementation notes:**
- Constructor should determine `%LOCALAPPDATA%` via `SHGetKnownFolderPath(FOLDERID_LocalAppData, ...)` or `winrt::Windows::Storage::ApplicationData::Current().LocalFolder().Path()`.
- Create the `Fling` directory if it doesn't exist.
- `IsConfigured()` returns `true` if the config file exists and has non-empty bucket + accessKey + secretKey.
- Use nlohmann-json `to_json` / `from_json` for FlingConfig serialization.

#### 2.2 — S3Uploader

Uploads a file to S3 and returns a presigned download URL.

```cpp
// S3Uploader.h
#pragma once
#include "Models/FlingConfig.h"
#include <string>
#include <functional>

struct UploadResult {
    bool success;
    std::wstring url;
    std::wstring s3Key;
    uint64_t fileSize;
    std::wstring errorMessage;
};

using ProgressCallback = std::function<void(uint64_t bytesTransferred, uint64_t totalBytes)>;

class S3Uploader {
public:
    explicit S3Uploader(const FlingConfig& config);
    ~S3Uploader();

    UploadResult Upload(const std::wstring& filePath, ProgressCallback progress = nullptr);

private:
    FlingConfig m_config;
    std::shared_ptr<Aws::S3::S3Client> m_client;
};
```

**Implementation notes:**
- **AWS SDK initialization:** `Aws::InitAPI()` must be called once at app startup (in `App.xaml.cpp`), not per-upload. `Aws::ShutdownAPI()` at app exit.
- **Client configuration:**
  - Set `config.endpointOverride` if `FlingConfig::endpoint` is non-empty.
  - Set `config.region`.
  - For MinIO/path-style: if `FlingConfig::usePathStyle` is true, configure the S3 client with `Aws::Client::AWSAuthV4Signer::PayloadSigningPolicy::Never` and `useVirtualAddressing = false`.
- **Key generation:** Use `Aws::Utils::UUID::RandomUUID()` + original filename. If `keyPrefix` is set, prepend it. Pattern: `{keyPrefix}{uuid}/{originalFilename}`.
- **Upload:** Use `PutObject`. Set `ContentType` based on file extension if possible (simple map of common types), otherwise `application/octet-stream`.
- **Presigned URL:** Call `m_client->GeneratePresignedUrl(bucket, key, HttpMethod::HTTP_GET, expirySeconds)`.
- **Progress:** The AWS SDK supports a `DataTransferredEventHandler` on the request. Wire this to the `ProgressCallback`.
- **Error handling:** Check `outcome.IsSuccess()`. If not, populate `errorMessage` from `outcome.GetError().GetMessage()`.

#### 2.3 — HistoryManager

Tracks upload history as a JSON array.

```cpp
// HistoryManager.h
#pragma once
#include "Models/UploadEntry.h"
#include <vector>

class HistoryManager {
public:
    HistoryManager();
    std::vector<UploadEntry> Load();
    void AddEntry(const UploadEntry& entry);
    void Clear();
    void RemoveExpired();
private:
    std::filesystem::path m_historyPath;
};
```

**Implementation notes:**
- Same `%LOCALAPPDATA%\Fling\` directory as config.
- Cap history at 500 entries. When adding beyond 500, remove the oldest.
- `RemoveExpired()` removes entries where `expiresAt < now`.

#### 2.4 — ClipboardHelper

```cpp
// ClipboardHelper.h
#pragma once
#include <string>

class ClipboardHelper {
public:
    static void CopyToClipboard(const std::wstring& text);
};
```

**Implementation notes:**
- Use `winrt::Windows::ApplicationModel::DataTransfer::DataPackage` and `Clipboard::SetContent()`.
- Alternatively, plain Win32 `OpenClipboard / SetClipboardData / CloseClipboard` works too and has no async complexity.

#### 2.5 — ToastHelper

```cpp
// ToastHelper.h
#pragma once
#include <string>

class ToastHelper {
public:
    static void ShowSuccess(const std::wstring& fileName, const std::wstring& url);
    static void ShowError(const std::wstring& fileName, const std::wstring& error);
};
```

**Implementation notes:**
- Use `winrt::Microsoft::Windows::AppNotifications::Builder::AppNotificationBuilder`.
- The toast for success should show: "Fling — Link copied!", the file name, and ideally have an action button "Open link" that opens the URL in the default browser.
- The toast for error should show: "Fling — Upload failed" and the error message.
- Register the `AppNotificationManager` in `App.xaml.cpp` `OnLaunched`.

#### 2.6 — Integration Test

Before building UI, test the full pipeline:

```cpp
// In App.xaml.cpp OnLaunched, temporarily:
auto config = ConfigManager().Load();
S3Uploader uploader(config);
auto result = uploader.Upload(L"C:\\test\\sample.pdf");
if (result.success) {
    ClipboardHelper::CopyToClipboard(result.url);
    ToastHelper::ShowSuccess(L"sample.pdf", result.url);
    HistoryManager().AddEntry({...});
}
```

Create a test config.json manually to verify.

---

### Phase 3: Context Menu Integration

**Goal:** Right-clicking a file in Explorer shows "Fling — Share file". Clicking it triggers the upload.

**Approach:** MSIX sparse package context menu registration via `Package.appxmanifest`.

#### 3.1 — Manifest Declaration

In `Package.appxmanifest`, add the file context menu extension. This requires the `desktop4` and `desktop5` namespaces:

```xml
<Package
  xmlns:desktop4="http://schemas.microsoft.com/appx/manifest/desktop/windows10/4"
  xmlns:desktop5="http://schemas.microsoft.com/appx/manifest/desktop/windows10/5"
  ...>

  <Applications>
    <Application ...>
      <Extensions>

        <!-- Context menu for all file types -->
        <desktop4:Extension Category="windows.fileExplorerContextMenus">
          <desktop4:FileExplorerContextMenus>
            <desktop5:ItemType Type="*">
              <desktop5:Verb Id="FlingShare"
                             Clsid="{generate-a-GUID}"
                             DelegateExecute="{same-GUID}"
                             DisplayName="Fling — Share file" />
            </desktop5:ItemType>
          </desktop4:FileExplorerContextMenus>
        </desktop4:Extension>

        <!-- App execution alias for activation -->
        <uap5:Extension Category="windows.appExecutionAlias">
          <uap5:AppExecutionAlias>
            <uap5:ExecutionAlias Alias="fling.exe" />
          </uap5:AppExecutionAlias>
        </uap5:Extension>

      </Extensions>
    </Application>
  </Applications>
</Package>
```

> **Important research note:** The exact mechanism for WinUI 3 / Windows App SDK context menu integration has evolved. You may need to use `IExplorerCommand` registration via the manifest, or handle activation via `AppInstance`. Research the latest Windows App SDK docs for `FileExplorerContextMenus` and `AppLifecycle` activation. The key concept is: Explorer launches your app (or activates a running instance) with the file path as an argument.

#### 3.2 — Activation Handling

In `App.xaml.cpp`, handle file activation:

```cpp
void App::OnLaunched(LaunchActivatedEventArgs const& args)
{
    // Check if launched with file arguments (from context menu)
    auto cmdLine = GetCommandLineW();
    // Parse to extract file path
    // If file path present: trigger upload pipeline
    // If no file path: show the main settings/history window
}
```

**Alternative approach:** Use `AppInstance::GetActivatedEventArgs()` which provides richer activation info including `FileActivatedEventArgs` if registered.

**Single instance:** Use `AppInstance` redirect to ensure only one instance runs. If a second activation comes in (another file right-click while uploading), it should queue the upload rather than launch a second process.

```cpp
// In main() or App startup
auto mainInstance = AppInstance::FindOrRegisterForKey(L"FlingMainInstance");
if (!mainInstance.IsCurrent()) {
    // Redirect to existing instance
    mainInstance.RedirectActivationToAsync(AppInstance::GetCurrent().GetActivatedEventArgs()).get();
    return;
}
// Register for future redirected activations
mainInstance.Activated([](auto&&, AppActivationArguments const& args) {
    // Handle file upload from redirected activation
});
```

---

### Phase 4: WinUI 3 Tray Application

**Goal:** A system tray application with a settings page and upload history.

#### 4.1 — System Tray Icon

WinUI 3 doesn't have built-in tray icon support. Use the Win32 `Shell_NotifyIconW` API:

```cpp
// In MainWindow.xaml.cpp
NOTIFYICONDATA nid = {};
nid.cbSize = sizeof(nid);
nid.hWnd = GetHWNDFromMainWindow();  // Get native HWND from WinUI window
nid.uFlags = NIF_ICON | NIF_MESSAGE | NIF_TIP;
nid.uCallbackMessage = WM_TRAYICON;  // Custom message ID
nid.hIcon = LoadIcon(GetModuleHandle(NULL), MAKEINTRESOURCE(IDI_FLING));
wcscpy_s(nid.szTip, L"Fling — File Sharing");
Shell_NotifyIconW(NIM_ADD, &nid);
```

**Getting HWND from WinUI 3 window:**
```cpp
auto windowNative = this->m_inner.as<::IWindowNative>();
HWND hwnd;
windowNative->get_WindowHandle(&hwnd);
```

**Tray icon behavior:**
- Left-click: toggle show/hide main window.
- Right-click: show a small context menu (Win32 `TrackPopupMenu`) with: "Open Fling", "Settings", "Upload History", separator, "Quit".

**On app launch:** Hide the main window, show only the tray icon. The main window opens when the user clicks the tray icon or "Open Fling".

**On window close (X button):** Hide the window instead of quitting. The app stays in the tray.

#### 4.2 — Settings Page

`SettingsPage.xaml` — A simple form for S3 configuration.

**XAML structure:**
```xml
<Page>
  <ScrollViewer>
    <StackPanel Padding="32" Spacing="16" MaxWidth="600">

      <TextBlock Text="S3 Configuration" Style="{StaticResource TitleTextBlockStyle}" />

      <TextBox x:Name="EndpointBox" Header="S3 Endpoint"
               PlaceholderText="https://s3.amazonaws.com (leave empty for AWS default)" />

      <TextBox x:Name="RegionBox" Header="Region"
               PlaceholderText="us-east-1" />

      <TextBox x:Name="BucketBox" Header="Bucket Name"
               PlaceholderText="my-fling-bucket" />

      <PasswordBox x:Name="AccessKeyBox" Header="Access Key ID"
                    PlaceholderText="AKIA..." />

      <PasswordBox x:Name="SecretKeyBox" Header="Secret Access Key"
                    PlaceholderText="wJalr..." />

      <ComboBox x:Name="ExpiryCombo" Header="Link Expiry">
        <!-- Items: 1 hour, 1 day, 7 days, 30 days -->
      </ComboBox>

      <ToggleSwitch x:Name="PathStyleToggle"
                     Header="Use path-style addressing"
                     IsOn="True"
                     OnContent="Enabled (required for MinIO)"
                     OffContent="Disabled (virtual-hosted)" />

      <Button Content="Test Connection" Click="OnTestConnection" />
      <Button Content="Save" Style="{StaticResource AccentButtonStyle}" Click="OnSave" />

      <InfoBar x:Name="StatusBar" IsOpen="False" />

    </StackPanel>
  </ScrollViewer>
</Page>
```

**Code-behind:**
- `OnNavigatedTo`: Load config from `ConfigManager`, populate fields.
- `OnSave`: Validate fields (bucket, accessKey, secretKey are required), save via `ConfigManager`.
- `OnTestConnection`: Create an `S3Uploader`, call `ListBuckets` or `HeadBucket` to verify credentials. Show success/failure in the `InfoBar`.

#### 4.3 — History Page

`HistoryPage.xaml` — A list of past uploads.

**XAML structure:**
```xml
<Page>
  <Grid>
    <Grid.RowDefinitions>
      <RowDefinition Height="Auto" />
      <RowDefinition Height="*" />
    </Grid.RowDefinitions>

    <StackPanel Orientation="Horizontal" Padding="32,16" Spacing="8">
      <TextBlock Text="Upload History" Style="{StaticResource TitleTextBlockStyle}" />
      <Button Content="Clear All" Click="OnClearHistory" />
    </StackPanel>

    <ListView Grid.Row="1" x:Name="HistoryList" Padding="32,0">
      <ListView.ItemTemplate>
        <DataTemplate>
          <Grid Padding="0,8" ColumnSpacing="12">
            <Grid.ColumnDefinitions>
              <ColumnDefinition Width="*" />
              <ColumnDefinition Width="Auto" />
              <ColumnDefinition Width="Auto" />
            </Grid.ColumnDefinitions>

            <StackPanel>
              <TextBlock Text="{Binding FileName}" FontWeight="SemiBold" />
              <TextBlock Text="{Binding FormattedSize}" Foreground="{ThemeResource TextFillColorSecondaryBrush}" />
              <TextBlock Text="{Binding FormattedDate}" Foreground="{ThemeResource TextFillColorSecondaryBrush}" />
            </StackPanel>

            <TextBlock Grid.Column="1" Text="{Binding ExpiryStatus}"
                       Foreground="{Binding ExpiryColor}" VerticalAlignment="Center" />

            <Button Grid.Column="2" Content="Copy Link" Click="OnCopyLink" VerticalAlignment="Center" />
          </Grid>
        </DataTemplate>
      </ListView.ItemTemplate>
    </ListView>
  </Grid>
</Page>
```

**Code-behind:**
- `OnNavigatedTo`: Load history from `HistoryManager`, bind to `HistoryList`.
- `OnCopyLink`: Copy the URL for that entry to clipboard.
- `OnClearHistory`: Confirm, then clear history file.
- Expired entries should show "Expired" in red, active entries show remaining time in green/default.

#### 4.4 — Upload Progress Dialog

When an upload is triggered (from context menu or drag-and-drop), show a small progress dialog:

```xml
<ContentDialog x:Name="UploadDialog" Title="Uploading...">
  <StackPanel Spacing="12" MinWidth="350">
    <TextBlock x:Name="FileNameText" />
    <ProgressBar x:Name="UploadProgress" Minimum="0" Maximum="100" />
    <TextBlock x:Name="ProgressText" Foreground="{ThemeResource TextFillColorSecondaryBrush}" />
  </StackPanel>
  <ContentDialog.SecondaryButtonText>Cancel</ContentDialog.SecondaryButtonText>
</ContentDialog>
```

**Behavior:**
- The progress callback from `S3Uploader` runs on a background thread. Use `DispatcherQueue` to marshal progress updates to the UI thread.
- On completion: dismiss dialog, copy URL, show toast.
- On cancel: abort the upload (the AWS SDK supports cancellation tokens).

---

### Phase 5: Polish & Edge Cases

#### 5.1 — Multi-file Support

Allow selecting multiple files in Explorer → right-click → Fling. Upload each sequentially (or in parallel with a configurable limit). The progress dialog should show a list of files with individual progress bars.

#### 5.2 — Drag and Drop

Allow dragging files onto the tray icon to trigger upload. Requires `IDropTarget` registration on the tray icon HWND.

#### 5.3 — Large File Handling

For files over 100 MB, use S3 multipart upload instead of single `PutObject`. The AWS SDK supports this via `TransferManager`:

```cpp
auto transferConfig = Aws::Transfer::TransferManagerConfiguration(executor.get());
transferConfig.s3Client = m_client;
auto transferManager = Aws::Transfer::TransferManager::Create(transferConfig);
auto handle = transferManager->UploadFile(filePath, bucket, key, contentType, metadata);
```

`TransferManager` handles chunking, parallel part uploads, and retry automatically.

#### 5.4 — Error Handling & Resilience

- **No config:** On first launch (or if config is missing), the context menu action should open the settings page instead of attempting upload.
- **Network errors:** Show toast with error. Offer retry in the upload history.
- **Credential errors (403):** Show toast suggesting the user check their credentials in settings.
- **Bucket not found (404):** Show toast suggesting the user check their bucket name.

#### 5.5 — Security

- **Credential storage:** Consider using Windows Credential Manager (`CredWrite` / `CredRead`) instead of plain-text JSON for `accessKey` and `secretKey`. This is a nice-to-have for v1 but important for any shared/public release.
- **MSIX isolation:** The MSIX package provides app container isolation, so `%LOCALAPPDATA%\Fling\` is actually within the package's virtualized filesystem.

#### 5.6 — Auto-start (Optional)

Add a startup task in the manifest so the tray app starts on login:

```xml
<desktop:Extension Category="windows.startupTask">
  <desktop:StartupTask TaskId="FlingStartup" Enabled="false" DisplayName="Fling" />
</desktop:Extension>
```

Expose a toggle in settings to enable/disable.

---

## Build & Dependency Notes

### vcpkg Dependencies

| Package | Purpose |
|---------|---------|
| `aws-sdk-cpp[s3]` | S3 upload, presigned URLs |
| `nlohmann-json` | Config and history JSON serialization |

### Windows App SDK (NuGet)

The WinUI 3 project template adds the Windows App SDK NuGet package automatically. Current stable version is 1.7.x. Ensure it's up to date.

### Build Configuration

- **Platform:** x64 (primary), ARM64 (optional later).
- **Configuration:** Debug for development, Release for distribution.
- **C++ standard:** C++20 (for `<format>`, `<filesystem>`, structured bindings, etc.).
- **vcpkg triplet:** `x64-windows` (dynamic linking).

### Key Compiler/Linker Settings

- `/await` or `/std:c++20` for coroutines (used by C++/WinRT).
- Link against: `WindowsApp.lib` (for WinRT), `aws-cpp-sdk-s3`, `aws-cpp-sdk-core`.
- Preprocessor: `WINRT_LEAN_AND_MEAN` (optional, reduces WinRT header bloat).

---

## Implementation Order (for Claude Code sessions)

Work through these in order. Each step should result in a buildable, testable state.

1. **Create blank WinUI 3 C++ project** and verify it builds and runs.
2. **Add vcpkg.json** and integrate aws-sdk-cpp[s3] + nlohmann-json. Verify `#include` works.
3. **Implement FlingConfig model** and **ConfigManager** (read/write JSON config).
4. **Implement S3Uploader** — PutObject + presigned URL. Test with hardcoded config.
5. **Implement ClipboardHelper** and **ToastHelper**.
6. **Wire up end-to-end** — hardcoded file path → upload → clipboard → toast.
7. **Implement SettingsPage XAML** and code-behind. Test saving/loading config.
8. **Implement HistoryManager** and **HistoryPage XAML**.
9. **Add context menu** via Package.appxmanifest. Handle file activation in App.xaml.cpp.
10. **Add single-instance** behavior with AppInstance redirect.
11. **Add system tray icon** with Win32 Shell_NotifyIcon.
12. **Add UploadProgressDialog** with background upload + UI thread progress.
13. **Add TransferManager** for large file multipart uploads.
14. **Polish:** error handling, credential validation, test connection button, multi-file support.

---

## Testing Checklist

- [ ] Upload a small file (< 5 MB) to AWS S3 → presigned URL works in browser
- [ ] Upload a small file to MinIO → presigned URL works
- [ ] Upload a large file (> 100 MB) → multipart upload, progress bar works
- [ ] Right-click a file in Explorer → "Fling" appears → upload works
- [ ] Right-click while app is already running → file queued to existing instance
- [ ] Settings page → save → reload → values persisted
- [ ] Test connection button → success/failure feedback
- [ ] History page → shows past uploads → copy link works
- [ ] Expired link → shows "Expired" in history
- [ ] Cancel upload mid-progress → upload aborted cleanly
- [ ] Close window → app stays in tray → tray icon click reopens
- [ ] App starts minimized to tray (no window flash)
- [ ] Toast notification → click "Open link" → browser opens URL
- [ ] No config on first launch → opens settings page
- [ ] Invalid credentials → clear error message

---

## Claude Code Prompt

Use the following prompt when starting a Claude Code session to work on this project. Copy everything below the line.

---

### PROMPT FOR CLAUDE CODE

```
You are helping me build "Fling", a native Windows file-sharing tool. The complete project plan is in the file `fling-project-plan.md` in the project root — read it first before doing anything.

**Tech stack:**
- C++ with C++/WinRT
- WinUI 3 (Windows App SDK) for UI
- aws-sdk-cpp (via vcpkg) for S3 uploads
- nlohmann-json (via vcpkg) for config/history serialization
- MSIX-packaged for native Windows 11 context menu integration

**Project structure:** Visual Studio MSBuild solution (not CMake). The project was created from the "Blank App, Packaged (WinUI 3 in Desktop)" C++ template.

**Current phase:** [UPDATE THIS — e.g., "Phase 2.2 — implementing S3Uploader"]

**What I need help with:** [UPDATE THIS — e.g., "Write the S3Uploader class that uploads a file and returns a presigned URL"]

**Important constraints:**
- This is a Visual Studio MSBuild project, not CMake. Don't generate CMakeLists.txt.
- Use C++/WinRT projections (winrt:: namespace), not C++/CX (hat pointers).
- aws-sdk-cpp is available via vcpkg integrate install. Headers are at <aws/...>.
- nlohmann-json is available via vcpkg. Header is <nlohmann/json.hpp>.
- Config is stored at %LOCALAPPDATA%\Fling\config.json.
- History is stored at %LOCALAPPDATA%\Fling\history.json.
- The app should be single-instance (use AppInstance redirect).
- AWS SDK must be initialized once in App.xaml.cpp, not per-upload.
- All S3 operations should support custom endpoints (for MinIO compatibility).
- Use path-style addressing when usePathStyle is true in config.
- Presigned URLs should respect the expirySeconds from config.
- Toast notifications use the Windows App SDK AppNotification API.
- The tray icon uses Win32 Shell_NotifyIconW (WinUI 3 has no built-in tray support).
- The context menu is registered via the MSIX manifest, not registry hacks.

**Code style:**
- Modern C++20.
- Use std::filesystem for file paths.
- Use winrt::hstring for WinRT interop strings, std::wstring elsewhere.
- Error handling: use return values (UploadResult with success/error), not exceptions for expected failures. Use exceptions for programmer errors.
- Keep header files clean — forward declare where possible, implementation in .cpp files.
- XAML files should use x:Bind where possible for compile-time binding.
```

---

## Reference Links

- [WinUI 3 documentation](https://learn.microsoft.com/en-us/windows/apps/winui/winui3/)
- [Windows App SDK overview](https://learn.microsoft.com/en-us/windows/apps/windows-app-sdk/)
- [Create your first WinUI 3 app](https://learn.microsoft.com/en-us/windows/apps/winui/winui3/create-your-first-winui3-app)
- [WinUI 3 Gallery (interactive samples)](https://github.com/microsoft/WinUI-Gallery)
- [aws-sdk-cpp GitHub](https://github.com/aws/aws-sdk-cpp)
- [aws-sdk-cpp S3 examples](https://docs.aws.amazon.com/sdk-for-cpp/v1/developer-guide/examples-s3.html)
- [vcpkg documentation](https://learn.microsoft.com/en-us/vcpkg/)
- [AppNotification (toast) API](https://learn.microsoft.com/en-us/windows/apps/windows-app-sdk/notifications/app-notifications/app-notifications-quickstart)
- [Shell_NotifyIcon (tray icon)](https://learn.microsoft.com/en-us/windows/win32/api/shellapi/nf-shellapi-shell_notifyiconw)
- [MSIX context menu integration](https://learn.microsoft.com/en-us/windows/apps/desktop/modernize/desktop-to-uwp-extensions)
- [AppInstance single-instancing](https://learn.microsoft.com/en-us/windows/apps/windows-app-sdk/applifecycle/applifecycle-instancing)
