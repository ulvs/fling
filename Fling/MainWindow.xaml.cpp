#include "pch.h"
#include "MainWindow.xaml.h"
#include "Pages/SettingsPage.xaml.h"
#include "Pages/HistoryPage.xaml.h"
#include "Core/ConfigManager.h"

#if __has_include("MainWindow.g.cpp")
#include "MainWindow.g.cpp"
#endif

#include <microsoft.ui.xaml.window.h>
#include <winrt/Microsoft.UI.Interop.h>
#include <winrt/Microsoft.UI.Xaml.Interop.h>

using namespace winrt;
using namespace winrt::Microsoft::UI::Xaml;
using namespace winrt::Microsoft::UI::Xaml::Controls;
using namespace winrt::Microsoft::UI::Windowing;

#define WM_TRAYICON (WM_USER + 1)
#define IDM_OPEN 1001
#define IDM_SETTINGS 1002
#define IDM_HISTORY 1003
#define IDM_QUIT 1004

namespace winrt::Fling::implementation
{
    MainWindow::MainWindow()
    {
        InitializeComponent();

        // Get HWND
        auto windowNative = this->try_as<IWindowNative>();
        if (windowNative)
        {
            windowNative->get_WindowHandle(&m_hwnd);
        }

        Title(L"Fling");
        AppWindow().SetIcon(L"Assets\\Fling.ico");

        // Mica runs under the title bar; caption buttons follow light/dark mode.
        ExtendsContentIntoTitleBar(true);
        SetTitleBar(AppTitleBar());
        AppWindow().TitleBar().PreferredTheme(winrt::Microsoft::UI::Windowing::TitleBarTheme::UseDefaultAppMode);
        PlaceAtDefaultSize();

        // Until storage is set up there's nothing to upload to, so start on Settings.
        SelectPage(::Fling::Core::ConfigManager().IsConfigured() ? L"uploads" : L"settings");
    }

    // Without a size, Windows makes a new window most of a large screen. Open at a comfortable
    // size instead (the content is at most 640 wide), centred on the screen with the cursor.
    void MainWindow::PlaceAtDefaultSize()
    {
        constexpr int kWidthDip = 920;
        constexpr int kHeightDip = 700;
        constexpr int kMinWidthDip = 480;
        constexpr int kMinHeightDip = 400;

        auto appWindow = AppWindow();
        double scale = GetDpiForWindow(m_hwnd) / 96.0;
        POINT cursor{};
        GetCursorPos(&cursor);
        auto area = DisplayArea::GetFromPoint({ cursor.x, cursor.y }, DisplayAreaFallback::Primary).WorkArea();

        int width = std::min(static_cast<int>(kWidthDip * scale), area.Width * 9 / 10);
        int height = std::min(static_cast<int>(kHeightDip * scale), area.Height * 9 / 10);
        appWindow.MoveAndResize({ area.X + (area.Width - width) / 2, area.Y + (area.Height - height) / 2, width, height });

        if (auto presenter = appWindow.Presenter().try_as<OverlappedPresenter>())
        {
            presenter.PreferredMinimumWidth(static_cast<int>(kMinWidthDip * scale));
            presenter.PreferredMinimumHeight(static_cast<int>(kMinHeightDip * scale));
        }
    }

    MainWindow::~MainWindow()
    {
        RemoveTrayIcon();
    }

    void MainWindow::SelectPage(hstring const& tag)
    {
        for (auto const& item : NavView().MenuItems())
        {
            auto navItem = item.try_as<NavigationViewItem>();
            if (navItem && unbox_value_or<hstring>(navItem.Tag(), L"") == tag)
            {
                NavView().SelectedItem(navItem);
                return;
            }
        }
    }

    void MainWindow::NavView_SelectionChanged(
        NavigationView const& /*sender*/,
        NavigationViewSelectionChangedEventArgs const& args)
    {
        auto selected = args.SelectedItem().try_as<NavigationViewItem>();
        if (!selected)
        {
            return;
        }

        auto tagBox = selected.Tag();
        auto tag = unbox_value_or<hstring>(tagBox, L"");

        auto frame = ContentFrame();
        if (!frame)
        {
            return;
        }

        if (tag == L"settings")
        {
            frame.Navigate(xaml_typename<Fling::SettingsPage>());
        }
        else if (tag == L"uploads")
        {
            frame.Navigate(xaml_typename<Fling::HistoryPage>());
        }
    }

    void MainWindow::SetupTrayIcon()
    {
        if (m_trayIconCreated || !m_hwnd)
            return;

        wchar_t exePath[MAX_PATH];
        GetModuleFileNameW(nullptr, exePath, MAX_PATH);

        m_notifyIconData = {};
        m_notifyIconData.cbSize = sizeof(NOTIFYICONDATAW);
        m_notifyIconData.hWnd = m_hwnd;
        m_notifyIconData.uID = 1;
        m_notifyIconData.uFlags = NIF_ICON | NIF_MESSAGE | NIF_TIP;
        m_notifyIconData.uCallbackMessage = WM_TRAYICON;
        m_notifyIconData.hIcon = ExtractIconW(GetModuleHandleW(nullptr), exePath, 0);
        wcscpy_s(m_notifyIconData.szTip, L"Fling - File Sharing");

        Shell_NotifyIconW(NIM_ADD, &m_notifyIconData);
        m_trayIconCreated = true;
    }

    void MainWindow::RemoveTrayIcon()
    {
        if (m_trayIconCreated)
        {
            Shell_NotifyIconW(NIM_DELETE, &m_notifyIconData);
            if (m_notifyIconData.hIcon)
            {
                DestroyIcon(m_notifyIconData.hIcon);
            }
            m_trayIconCreated = false;
        }
    }

    void MainWindow::ShowWindow()
    {
        if (m_hwnd)
        {
            ::ShowWindow(m_hwnd, SW_SHOW);
            ::SetForegroundWindow(m_hwnd);
        }
    }

    void MainWindow::HideWindow()
    {
        if (m_hwnd)
        {
            ::ShowWindow(m_hwnd, SW_HIDE);
        }
    }

    void MainWindow::ShowTrayContextMenu()
    {
        POINT pt;
        GetCursorPos(&pt);

        HMENU hMenu = CreatePopupMenu();
        AppendMenuW(hMenu, MF_STRING, IDM_OPEN, L"Open Fling");
        AppendMenuW(hMenu, MF_SEPARATOR, 0, nullptr);
        AppendMenuW(hMenu, MF_STRING, IDM_SETTINGS, L"Settings");
        AppendMenuW(hMenu, MF_STRING, IDM_HISTORY, L"History");
        AppendMenuW(hMenu, MF_SEPARATOR, 0, nullptr);
        AppendMenuW(hMenu, MF_STRING, IDM_QUIT, L"Quit");

        SetForegroundWindow(m_hwnd);
        UINT cmd = TrackPopupMenu(hMenu, TPM_RETURNCMD | TPM_NONOTIFY, pt.x, pt.y, 0, m_hwnd, nullptr);
        DestroyMenu(hMenu);

        switch (cmd)
        {
        case IDM_OPEN:
        case IDM_SETTINGS:
        case IDM_HISTORY:
            ShowWindow();
            break;
        case IDM_QUIT:
            RemoveTrayIcon();
            Application::Current().Exit();
            break;
        }
    }

    void MainWindow::HandleTrayMessage(WPARAM /*wParam*/, LPARAM lParam)
    {
        switch (LOWORD(lParam))
        {
        case WM_LBUTTONUP:
            ShowWindow();
            break;
        case WM_RBUTTONUP:
            ShowTrayContextMenu();
            break;
        }
    }

    void MainWindow::QueueUpload(std::wstring const& /*filePath*/)
    {
        // TODO: Implement upload queue in Phase 3
    }
}
