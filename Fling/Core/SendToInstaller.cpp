// NOTE: this translation unit deliberately does NOT include pch.h. <shlobj.h>
// and <objbase.h> pull in a Windows-SDK IUnknown that clashes with the
// CppWinRT-flavored IUnknown brought in by pch.h. Keeping this file on its own
// compilation island (PrecompiledHeader=NotUsing in the vcxproj) sidesteps
// the conflict.

#include <windows.h>
#include <shlobj.h>
#include <objbase.h>
#include <filesystem>
#include <string>

#include "SendToInstaller.h"

namespace
{
    constexpr wchar_t kShortcutFileName[] = L"Fling.lnk";
    constexpr wchar_t kShortcutDescription[] = L"Fling - Share file to S3";

    std::wstring GetExePath()
    {
        wchar_t buf[MAX_PATH];
        DWORD len = GetModuleFileNameW(nullptr, buf, MAX_PATH);
        return std::wstring(buf, len);
    }

    std::filesystem::path GetSendToFolder()
    {
        PWSTR path = nullptr;
        if (SUCCEEDED(SHGetKnownFolderPath(FOLDERID_SendTo, 0, nullptr, &path)))
        {
            std::filesystem::path result(path);
            CoTaskMemFree(path);
            return result;
        }
        return {};
    }

    std::filesystem::path GetShortcutPath()
    {
        auto folder = GetSendToFolder();
        if (folder.empty()) return {};
        return folder / kShortcutFileName;
    }

    // RAII helper to ensure CoUninitialize is called on scope exit if we
    // were the ones who initialized.
    struct ComScope
    {
        bool owned{ false };
        ComScope()
        {
            HRESULT hr = CoInitializeEx(nullptr, COINIT_APARTMENTTHREADED);
            owned = SUCCEEDED(hr);
        }
        ~ComScope() { if (owned) CoUninitialize(); }
    };
}

namespace Fling::Core
{
    bool SendToInstaller::Install()
    {
        auto exe = GetExePath();
        auto shortcut = GetShortcutPath();
        if (exe.empty() || shortcut.empty()) return false;

        auto workingDir = std::filesystem::path(exe).parent_path().wstring();

        ComScope com;

        IShellLinkW* shellLink = nullptr;
        HRESULT hr = CoCreateInstance(
            CLSID_ShellLink, nullptr, CLSCTX_INPROC_SERVER,
            IID_IShellLinkW, reinterpret_cast<void**>(&shellLink));
        if (FAILED(hr) || !shellLink) return false;

        bool ok = false;
        do
        {
            if (FAILED(shellLink->SetPath(exe.c_str()))) break;
            if (FAILED(shellLink->SetWorkingDirectory(workingDir.c_str()))) break;
            if (FAILED(shellLink->SetDescription(kShortcutDescription))) break;

            IPersistFile* persist = nullptr;
            if (FAILED(shellLink->QueryInterface(IID_IPersistFile, reinterpret_cast<void**>(&persist))) || !persist)
            {
                break;
            }

            HRESULT saveHr = persist->Save(shortcut.c_str(), TRUE);
            persist->Release();
            ok = SUCCEEDED(saveHr);
        } while (false);

        shellLink->Release();
        return ok;
    }

    bool SendToInstaller::Uninstall()
    {
        auto shortcut = GetShortcutPath();
        if (shortcut.empty()) return false;

        std::error_code ec;
        if (!std::filesystem::exists(shortcut, ec))
        {
            return true; // Already absent — treat as success.
        }
        return std::filesystem::remove(shortcut, ec);
    }

    bool SendToInstaller::IsInstalled()
    {
        auto shortcut = GetShortcutPath();
        if (shortcut.empty()) return false;
        std::error_code ec;
        return std::filesystem::exists(shortcut, ec);
    }
}
