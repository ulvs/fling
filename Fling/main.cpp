#include "pch.h"
#include <winrt/Microsoft.UI.Xaml.h>
#include <winrt/Microsoft.Windows.AppLifecycle.h>
#include "App.xaml.h"

#include <thread>

using namespace winrt::Microsoft::Windows::AppLifecycle;

int WINAPI wWinMain(
    _In_ HINSTANCE /*hInstance*/,
    _In_opt_ HINSTANCE /*hPrevInstance*/,
    _In_ PWSTR /*pCmdLine*/,
    _In_ int /*nCmdShow*/)
{
    winrt::init_apartment(winrt::apartment_type::single_threaded);

    // Single-instance: the first Fling process that wins FindOrRegisterForKey
    // owns the app. Subsequent launches (e.g. a second context-menu invocation)
    // redirect their activation to that process and exit.
    auto activationArgs = AppInstance::GetCurrent().GetActivatedEventArgs();
    auto keyInstance = AppInstance::FindOrRegisterForKey(L"FlingMainInstance");

    if (!keyInstance.IsCurrent())
    {
        // Explorer gave this process foreground rights; pass them on so the running
        // instance's send window comes to the front instead of flashing in the taskbar.
        AllowSetForegroundWindow(ASFW_ANY);

        // Blocking this STA on the redirect deadlocks it (the call needs the apartment to pump),
        // so redirect on a worker thread and pump here until it completes.
        HANDLE redirected = CreateEventW(nullptr, TRUE, FALSE, nullptr);
        std::thread worker([&]
        {
            winrt::init_apartment();
            keyInstance.RedirectActivationToAsync(activationArgs).get();
            SetEvent(redirected);
            winrt::uninit_apartment();
        });
        DWORD signaled = 0;
        CoWaitForMultipleObjects(CWMO_DEFAULT, INFINITE, 1, &redirected, &signaled);
        worker.join();
        CloseHandle(redirected);
        return 0;
    }

    // We're the owner. Hook the Activated event so redirections from other
    // launches come back to us and get processed as uploads.
    keyInstance.Activated([](
        winrt::Windows::Foundation::IInspectable const&,
        AppActivationArguments const& args)
    {
        ::winrt::Fling::implementation::App::HandleRedirectedActivation(args);
    });

    ::winrt::Microsoft::UI::Xaml::Application::Start(
        [](auto&&)
        {
            ::winrt::make<::winrt::Fling::implementation::App>();
        });

    return 0;
}
