#pragma once
#include "MainWindow.g.h"

namespace winrt::Fling::implementation
{
    struct MainWindow : MainWindowT<MainWindow>
    {
        MainWindow();
        ~MainWindow();

        void NavView_SelectionChanged(
            winrt::Microsoft::UI::Xaml::Controls::NavigationView const& sender,
            winrt::Microsoft::UI::Xaml::Controls::NavigationViewSelectionChangedEventArgs const& args);

        // Select a navigation page by tag ("uploads", "settings").
        void SelectPage(winrt::hstring const& tag);

        // Tray icon management
        void SetupTrayIcon();
        void RemoveTrayIcon();
        void ShowWindow();
        void HideWindow();

        // Upload handling
        void QueueUpload(std::wstring const& filePath);

    private:
        HWND m_hwnd{ nullptr };
        NOTIFYICONDATAW m_notifyIconData{};
        bool m_trayIconCreated{ false };

        static LRESULT CALLBACK TrayWndProc(HWND hWnd, UINT message, WPARAM wParam, LPARAM lParam);
        void HandleTrayMessage(WPARAM wParam, LPARAM lParam);
        void ShowTrayContextMenu();
        void PlaceAtDefaultSize();
    };
}

namespace winrt::Fling::factory_implementation
{
    struct MainWindow : MainWindowT<MainWindow, implementation::MainWindow>
    {
    };
}
