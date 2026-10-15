#include <windows.h>
#include <shellapi.h>
#include <shlobj_core.h>
#include <shobjidl_core.h>
#include <shlwapi.h>
#include <strsafe.h>
#include <wrl/client.h>
#include <wrl/implements.h>
#include <wrl/module.h>

#include <filesystem>
#include <string>
#include <vector>

using Microsoft::WRL::ClassicCom;
using Microsoft::WRL::ComPtr;
using Microsoft::WRL::InProc;
using Microsoft::WRL::Module;
using Microsoft::WRL::RuntimeClass;
using Microsoft::WRL::RuntimeClassFlags;

namespace
{
    constexpr wchar_t kTitle[] = L"Fling it";
    constexpr wchar_t kVerbW[] = L"flingshare";
    constexpr char kVerbA[] = "flingshare";

    std::filesystem::path ModuleDirectory()
    {
        HMODULE module{};
        GetModuleHandleExW(GET_MODULE_HANDLE_EX_FLAG_FROM_ADDRESS | GET_MODULE_HANDLE_EX_FLAG_UNCHANGED_REFCOUNT,
            reinterpret_cast<LPCWSTR>(&ModuleDirectory), &module);
        std::wstring path(MAX_PATH, L'\0');
        for (;;)
        {
            DWORD length = GetModuleFileNameW(module, path.data(), static_cast<DWORD>(path.size()));
            if (length < path.size())
            {
                path.resize(length);
                break;
            }
            path.resize(path.size() * 2);
        }
        return std::filesystem::path(path).parent_path();
    }

    // The host (Explorer, Total Commander, ...) has no package identity, so launching Fling.exe by
    // path would run it unpackaged. The app execution alias activates it as the packaged app.
    std::filesystem::path FlingLauncher()
    {
        PWSTR localAppData{};
        if (SUCCEEDED(SHGetKnownFolderPath(FOLDERID_LocalAppData, 0, nullptr, &localAppData)))
        {
            std::filesystem::path alias = std::filesystem::path(localAppData) / L"Microsoft" / L"WindowsApps" / L"fling.exe";
            CoTaskMemFree(localAppData);
            std::error_code ec;
            if (std::filesystem::exists(alias, ec))
            {
                return alias;
            }
        }
        return ModuleDirectory() / L"Fling.exe";
    }

    std::wstring Quoted(std::wstring const& value)
    {
        return L"\"" + value + L"\"";
    }

    HRESULT LaunchFling(std::vector<std::wstring> const& paths)
    {
        if (paths.empty())
        {
            return S_OK;
        }
        std::wstring commandLine = Quoted(FlingLauncher().wstring());
        for (auto const& path : paths)
        {
            commandLine += L" " + Quoted(path);
        }

        STARTUPINFOW startup{ sizeof(startup) };
        PROCESS_INFORMATION process{};
        if (!CreateProcessW(nullptr, commandLine.data(), nullptr, nullptr, FALSE, 0, nullptr, nullptr, &startup, &process))
        {
            return HRESULT_FROM_WIN32(GetLastError());
        }
        CloseHandle(process.hThread);
        CloseHandle(process.hProcess);
        return S_OK;
    }

    bool HostIsExplorer()
    {
        wchar_t path[MAX_PATH]{};
        GetModuleFileNameW(nullptr, path, MAX_PATH);
        return _wcsicmp(PathFindFileNameW(path), L"explorer.exe") == 0;
    }
}

// Windows 11 top-level menu entry (Explorer).
class __declspec(uuid("57F4F289-F50A-441E-83F8-786B9DAC942E")) FlingShareCommand final
    : public RuntimeClass<RuntimeClassFlags<ClassicCom>, IExplorerCommand>
{
public:
    IFACEMETHODIMP GetTitle(IShellItemArray*, PWSTR* name) override
    {
        return SHStrDupW(kTitle, name);
    }

    IFACEMETHODIMP GetIcon(IShellItemArray*, PWSTR* icon) override
    {
        return SHStrDupW((ModuleDirectory() / L"Assets" / L"Fling.ico").c_str(), icon);
    }

    IFACEMETHODIMP GetToolTip(IShellItemArray*, PWSTR* tip) override
    {
        *tip = nullptr;
        return E_NOTIMPL;
    }

    IFACEMETHODIMP GetCanonicalName(GUID* guid) override
    {
        *guid = __uuidof(FlingShareCommand);
        return S_OK;
    }

    IFACEMETHODIMP GetState(IShellItemArray*, BOOL, EXPCMDSTATE* state) override
    {
        // Other hosts (some Windows builds merge this into their classic menu) get FlingClassicMenu instead.
        *state = HostIsExplorer() ? ECS_ENABLED : ECS_HIDDEN;
        return S_OK;
    }

    IFACEMETHODIMP Invoke(IShellItemArray* items, IBindCtx*) override
    {
        if (!items)
        {
            return E_INVALIDARG;
        }

        DWORD count = 0;
        HRESULT hr = items->GetCount(&count);
        if (FAILED(hr))
        {
            return hr;
        }

        std::vector<std::wstring> paths;
        for (DWORD i = 0; i < count; ++i)
        {
            ComPtr<IShellItem> item;
            PWSTR path{};
            if (SUCCEEDED(items->GetItemAt(i, &item)) && SUCCEEDED(item->GetDisplayName(SIGDN_FILESYSPATH, &path)))
            {
                paths.emplace_back(path);
                CoTaskMemFree(path);
            }
        }
        return LaunchFling(paths);
    }

    IFACEMETHODIMP GetFlags(EXPCMDFLAGS* flags) override
    {
        *flags = ECF_DEFAULT;
        return S_OK;
    }

    IFACEMETHODIMP EnumSubCommands(IEnumExplorerCommand** commands) override
    {
        *commands = nullptr;
        return E_NOTIMPL;
    }
};

// Classic IContextMenu entry for every host except Explorer (Total Commander, other file managers).
// Exactly one of the two classes shows the entry in any host.
class __declspec(uuid("797BAD2C-1AE6-4D15-B7D1-82DC7868E6D3")) FlingClassicMenu final
    : public RuntimeClass<RuntimeClassFlags<ClassicCom>, IShellExtInit, IContextMenu>
{
public:
    IFACEMETHODIMP Initialize(PCIDLIST_ABSOLUTE, IDataObject* data, HKEY) override
    {
        m_paths.clear();
        if (!data)
        {
            return E_INVALIDARG;
        }

        FORMATETC format{ CF_HDROP, nullptr, DVASPECT_CONTENT, -1, TYMED_HGLOBAL };
        STGMEDIUM medium{};
        if (FAILED(data->GetData(&format, &medium)))
        {
            return E_INVALIDARG;
        }
        if (auto drop = static_cast<HDROP>(GlobalLock(medium.hGlobal)))
        {
            UINT count = DragQueryFileW(drop, 0xFFFFFFFF, nullptr, 0);
            for (UINT i = 0; i < count; ++i)
            {
                UINT length = DragQueryFileW(drop, i, nullptr, 0);
                std::wstring path(length, L'\0');
                DragQueryFileW(drop, i, path.data(), length + 1);
                m_paths.push_back(std::move(path));
            }
            GlobalUnlock(medium.hGlobal);
        }
        ReleaseStgMedium(&medium);
        return m_paths.empty() ? E_INVALIDARG : S_OK;
    }

    IFACEMETHODIMP QueryContextMenu(HMENU menu, UINT index, UINT firstId, UINT, UINT flags) override
    {
        if ((flags & CMF_DEFAULTONLY) || m_paths.empty() || HostIsExplorer())
        {
            return MAKE_HRESULT(SEVERITY_SUCCESS, 0, 0);
        }
        InsertMenuW(menu, index, MF_BYPOSITION | MF_STRING, firstId, kTitle);
        return MAKE_HRESULT(SEVERITY_SUCCESS, 0, 1);
    }

    IFACEMETHODIMP InvokeCommand(CMINVOKECOMMANDINFO* info) override
    {
        if (!info)
        {
            return E_INVALIDARG;
        }
        if (IS_INTRESOURCE(info->lpVerb))
        {
            if (LOWORD(info->lpVerb) != 0)
            {
                return E_FAIL;
            }
        }
        else if (_stricmp(info->lpVerb, kVerbA) != 0)
        {
            return E_FAIL;
        }
        return LaunchFling(m_paths);
    }

    IFACEMETHODIMP GetCommandString(UINT_PTR id, UINT type, UINT*, CHAR* name, UINT size) override
    {
        if (id != 0)
        {
            return E_INVALIDARG;
        }
        switch (type)
        {
        case GCS_VERBW:
            return StringCchCopyW(reinterpret_cast<LPWSTR>(name), size, kVerbW);
        case GCS_VERBA:
            return StringCchCopyA(name, size, kVerbA);
        case GCS_HELPTEXTW:
            return StringCchCopyW(reinterpret_cast<LPWSTR>(name), size, L"Upload to your storage and copy a link");
        default:
            return E_NOTIMPL;
        }
    }

private:
    std::vector<std::wstring> m_paths;
};

CoCreatableClass(FlingShareCommand)
CoCreatableClass(FlingClassicMenu)

STDAPI DllCanUnloadNow()
{
    return Module<InProc>::GetModule().GetObjectCount() == 0 ? S_OK : S_FALSE;
}

STDAPI DllGetClassObject(REFCLSID clsid, REFIID riid, _COM_Outptr_ void** instance)
{
    return Module<InProc>::GetModule().GetClassObject(clsid, riid, instance);
}

BOOL APIENTRY DllMain(HMODULE module, DWORD reason, LPVOID)
{
    if (reason == DLL_PROCESS_ATTACH)
    {
        DisableThreadLibraryCalls(module);
    }
    return TRUE;
}
