#pragma once
#include "App.xaml.g.h"
#include <memory>

namespace Aws { struct SDKOptions; }

namespace winrt::Fling::implementation
{
    struct App : AppT<App>
    {
        App();
        ~App();

        void OnLaunched(Microsoft::UI::Xaml::LaunchActivatedEventArgs const&);

        // Handle file activation from context menu
        void ProcessCommandLineArgs();
        void ProcessFilePath(std::wstring const& filePath);

        // HWND of the main window, so pages can attach FilePickers etc.
        // Populated in OnLaunched once the native window handle is available.
        static HWND MainWindowHwnd() { return s_mainHwnd; }

        // Dispatcher for the main (UI) thread. Used by the single-instance
        // activation handler to marshal redirected uploads onto the UI thread,
        // since ClipboardHelper requires it.
        static winrt::Microsoft::UI::Dispatching::DispatcherQueue MainDispatcher() { return s_dispatcher; }

        // Invoked for a redirected activation from a second launch. Parses the
        // command line from the activation args and triggers an upload.
        static void HandleRedirectedActivation(
            winrt::Microsoft::Windows::AppLifecycle::AppActivationArguments const& args);

    private:
        static HWND s_mainHwnd;
        static winrt::Microsoft::UI::Dispatching::DispatcherQueue s_dispatcher;

        winrt::Microsoft::UI::Xaml::Window m_window{ nullptr };
        std::unique_ptr<Aws::SDKOptions> m_awsOptions;
        bool m_awsInitialized{ false };

        void InitializeAwsSdk();
        void ShutdownAwsSdk();
        void RegisterAppNotifications();
    };
}
