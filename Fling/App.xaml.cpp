#include "pch.h"
#include "App.xaml.h"
#include "MainWindow.xaml.h"
#include "Core/ConfigManager.h"
#include "Core/UploadCoordinator.h"
#include <microsoft.ui.xaml.window.h>

#if __has_include("App.g.cpp")
#include "App.g.cpp"
#endif

using namespace winrt;
using namespace winrt::Windows::Foundation;
using namespace winrt::Microsoft::UI::Xaml;
using namespace winrt::Microsoft::Windows::AppLifecycle;
using namespace winrt::Microsoft::Windows::AppNotifications;

namespace winrt::Fling::implementation
{
    HWND App::s_mainHwnd = nullptr;
    winrt::Microsoft::UI::Dispatching::DispatcherQueue App::s_dispatcher{ nullptr };

    App::App()
    {
        InitializeAwsSdk();
        RegisterAppNotifications();

#if defined _DEBUG && !defined DISABLE_XAML_GENERATED_BREAK_ON_UNHANDLED_EXCEPTION
        UnhandledException([this](IInspectable const&, UnhandledExceptionEventArgs const& e)
        {
            if (IsDebuggerPresent())
            {
                auto errorMessage = e.Message();
                __debugbreak();
            }
        });
#endif
    }

    App::~App()
    {
        ShutdownAwsSdk();
    }

    void App::OnLaunched(LaunchActivatedEventArgs const&)
    {
        m_window = make<MainWindow>();

        if (auto native = m_window.try_as<IWindowNative>())
        {
            native->get_WindowHandle(&s_mainHwnd);
        }

        s_dispatcher = winrt::Microsoft::UI::Dispatching::DispatcherQueue::GetForCurrentThread();

        m_window.Activate();

        // Process any files passed on the command line (context-menu activation).
        // Done after the window is up so toast/clipboard have a live app to post to.
        ProcessCommandLineArgs();
    }

    void App::HandleRedirectedActivation(
        winrt::Microsoft::Windows::AppLifecycle::AppActivationArguments const& args)
    {
        if (!s_dispatcher)
        {
            return;
        }

        using namespace winrt::Microsoft::Windows::AppLifecycle;
        if (args.Kind() != ExtendedActivationKind::Launch)
        {
            return;
        }

        auto launch = args.Data().try_as<winrt::Microsoft::UI::Xaml::LaunchActivatedEventArgs>();
        if (!launch)
        {
            return;
        }

        std::wstring argText(launch.Arguments());
        if (argText.empty())
        {
            return;
        }

        // Marshal onto the UI thread. Clipboard access in UploadCoordinator
        // requires it, and we also want upload-completion toasts to appear
        // ordered with any UI work the user is doing.
        s_dispatcher.TryEnqueue([argText]()
        {
            int argc = 0;
            LPWSTR* argv = CommandLineToArgvW(argText.c_str(), &argc);
            if (!argv)
            {
                return;
            }
            for (int i = 0; i < argc; ++i)
            {
                std::wstring arg(argv[i]);
                if (!arg.empty() && arg[0] != L'-' && std::filesystem::exists(arg))
                {
                    ::Fling::Core::UploadCoordinator::UploadAndShare(std::filesystem::path(arg));
                }
            }
            LocalFree(argv);
        });
    }

    void App::ProcessCommandLineArgs()
    {
        // Get command line arguments
        int argc = 0;
        LPWSTR* argv = CommandLineToArgvW(GetCommandLineW(), &argc);

        if (argv != nullptr)
        {
            // Skip first arg (executable path), process file paths
            for (int i = 1; i < argc; ++i)
            {
                std::wstring arg(argv[i]);
                if (!arg.empty() && arg[0] != L'-')
                {
                    ProcessFilePath(arg);
                }
            }
            LocalFree(argv);
        }
    }

    void App::ProcessFilePath(std::wstring const& filePath)
    {
        if (!std::filesystem::exists(filePath))
        {
            return;
        }
        ::Fling::Core::UploadCoordinator::UploadAndShare(std::filesystem::path(filePath));
    }

    void App::InitializeAwsSdk()
    {
        if (m_awsInitialized)
        {
            return;
        }
        m_awsOptions = std::make_unique<Aws::SDKOptions>();
        Aws::InitAPI(*m_awsOptions);
        m_awsInitialized = true;
    }

    void App::ShutdownAwsSdk()
    {
        if (!m_awsInitialized)
        {
            return;
        }
        Aws::ShutdownAPI(*m_awsOptions);
        m_awsInitialized = false;
        m_awsOptions.reset();
    }

    void App::RegisterAppNotifications()
    {
        auto notificationManager = AppNotificationManager::Default();
        notificationManager.Register();
    }
}
