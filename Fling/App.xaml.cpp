#include "pch.h"
#include "App.xaml.h"
#include "MainWindow.xaml.h"
#include "SendWindow.xaml.h"
#include "Core/ConfigManager.h"
#include "Core/Log.h"
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
    winrt::Fling::MainWindow App::s_mainWindow{ nullptr };
    std::vector<winrt::Fling::SendWindow> App::s_sendWindows;

    App::App()
    {
        ::Fling::Core::Log::Info(L"Fling started");
        InitializeAwsSdk();
        RegisterAppNotifications();

        UnhandledException([](IInspectable const&, UnhandledExceptionEventArgs const& e)
        {
            ::Fling::Core::Log::Error(L"Unhandled exception: " + std::wstring(e.Message()));
#if defined _DEBUG && !defined DISABLE_XAML_GENERATED_BREAK_ON_UNHANDLED_EXCEPTION
            if (IsDebuggerPresent())
            {
                __debugbreak();
            }
#endif
        });
    }

    App::~App()
    {
        ShutdownAwsSdk();
    }

    void App::OnLaunched(LaunchActivatedEventArgs const&)
    {
        s_dispatcher = winrt::Microsoft::UI::Dispatching::DispatcherQueue::GetForCurrentThread();

        auto files = FilesFromArguments(GetCommandLineW(), /*skipProgramName=*/true);
        if (files.empty())
        {
            ShowMainWindow();
        }
        else
        {
            ShowSendWindow(std::move(files));
        }
    }

    void App::HandleRedirectedActivation(
        winrt::Microsoft::Windows::AppLifecycle::AppActivationArguments const& args)
    {
        if (!s_dispatcher)
        {
            return;
        }

        using namespace winrt::Microsoft::Windows::AppLifecycle;
        std::wstring argText;
        if (args.Kind() == ExtendedActivationKind::Launch)
        {
            // AppLifecycle hands over the Windows activation args, not the XAML ones.
            if (auto launch = args.Data().try_as<winrt::Windows::ApplicationModel::Activation::ILaunchActivatedEventArgs>())
            {
                argText = launch.Arguments();
            }
        }

        // Activated fires on a background thread; windows must be created on the UI thread.
        s_dispatcher.TryEnqueue([argText]()
        {
            auto files = FilesFromArguments(argText, /*skipProgramName=*/false);
            if (files.empty())
            {
                ShowMainWindow();
            }
            else
            {
                ShowSendWindow(std::move(files));
            }
        });
    }

    std::vector<std::filesystem::path> App::FilesFromArguments(std::wstring const& commandLine, bool skipProgramName)
    {
        std::vector<std::filesystem::path> files;
        if (commandLine.empty())
        {
            return files;
        }

        int argc = 0;
        LPWSTR* argv = CommandLineToArgvW(commandLine.c_str(), &argc);
        if (!argv)
        {
            return files;
        }
        for (int i = skipProgramName ? 1 : 0; i < argc; ++i)
        {
            std::wstring arg(argv[i]);
            std::error_code ec;
            // Options (including the toast activation marker) start with '-'.
            if (!arg.empty() && arg[0] != L'-' && (std::filesystem::is_regular_file(arg, ec) || std::filesystem::is_directory(arg, ec)))
            {
                files.emplace_back(arg);
            }
        }
        LocalFree(argv);
        return files;
    }

    void App::ShowMainWindow(hstring const& page)
    {
        if (!s_mainWindow)
        {
            s_mainWindow = make<MainWindow>();
            s_mainWindow.as<IWindowNative>()->get_WindowHandle(&s_mainHwnd);
            s_mainWindow.Closed([](auto&&, auto&&)
            {
                s_mainWindow = nullptr;
                s_mainHwnd = nullptr;
            });
        }

        if (!page.empty())
        {
            get_self<MainWindow>(s_mainWindow)->SelectPage(page);
        }

        if (auto presenter = s_mainWindow.AppWindow().Presenter().try_as<winrt::Microsoft::UI::Windowing::OverlappedPresenter>();
            presenter && presenter.State() == winrt::Microsoft::UI::Windowing::OverlappedPresenterState::Minimized)
        {
            presenter.Restore();
        }
        s_mainWindow.Activate();
    }

    void App::ShowSendWindow(std::vector<std::filesystem::path> files)
    {
        size_t folders = 0;
        for (auto const& item : files)
        {
            std::error_code ec;
            folders += std::filesystem::is_directory(item, ec) ? 1 : 0;
        }
        ::Fling::Core::Log::Info(L"Sharing " + std::to_wstring(files.size() - folders) + L" file(s) and "
            + std::to_wstring(folders) + L" folder(s)");
        auto window = make<SendWindow>();
        s_sendWindows.push_back(window);
        window.Closed([](IInspectable const& sender, WindowEventArgs const&)
        {
            auto closed = sender.as<winrt::Fling::SendWindow>();
            std::erase(s_sendWindows, closed);
        });
        get_self<SendWindow>(window)->Start(std::move(files));
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
        // Unpackaged self-contained builds can fail here (runtime resources missing);
        // uploads still work, only the toast is lost.
        try
        {
            AppNotificationManager::Default().Register();
        }
        catch (winrt::hresult_error const& e)
        {
            ::Fling::Core::Log::Warning(L"Notifications unavailable: " + std::wstring(e.message()));
        }
    }
}
