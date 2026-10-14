#pragma once
#include "App.xaml.g.h"
#include <winrt/Fling.h>
#include <filesystem>
#include <memory>
#include <vector>

namespace Aws { struct SDKOptions; }

namespace winrt::Fling::implementation
{
    struct App : AppT<App>
    {
        App();
        ~App();

        void OnLaunched(Microsoft::UI::Xaml::LaunchActivatedEventArgs const&);

        // Main window (Settings/History), created on first use. page: "settings", "history" or empty.
        static void ShowMainWindow(hstring const& page = L"");

        // Compact window that uploads the given files (Explorer "Share with Fling").
        static void ShowSendWindow(std::vector<std::filesystem::path> files);

        // HWND of the main window while it exists, so pages can attach FilePickers etc.
        static HWND MainWindowHwnd() { return s_mainHwnd; }

        // Dispatcher for the UI thread; redirected activations arrive on a background thread.
        static winrt::Microsoft::UI::Dispatching::DispatcherQueue MainDispatcher() { return s_dispatcher; }

        // Invoked for a redirected activation from a second launch: files open a send
        // window, a plain launch brings up the main window.
        static void HandleRedirectedActivation(
            winrt::Microsoft::Windows::AppLifecycle::AppActivationArguments const& args);

    private:
        static HWND s_mainHwnd;
        static winrt::Microsoft::UI::Dispatching::DispatcherQueue s_dispatcher;

        static winrt::Fling::MainWindow s_mainWindow;
        static std::vector<winrt::Fling::SendWindow> s_sendWindows;
        std::unique_ptr<Aws::SDKOptions> m_awsOptions;
        bool m_awsInitialized{ false };

        static std::vector<std::filesystem::path> FilesFromArguments(std::wstring const& commandLine, bool skipProgramName);
        void InitializeAwsSdk();
        void ShutdownAwsSdk();
        void RegisterAppNotifications();
    };
}
