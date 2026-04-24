#include "pch.h"
#include "ToastHelper.h"

#include <winrt/Microsoft.Windows.AppNotifications.h>
#include <winrt/Microsoft.Windows.AppNotifications.Builder.h>

using namespace winrt::Microsoft::Windows::AppNotifications;
using namespace winrt::Microsoft::Windows::AppNotifications::Builder;

namespace Fling::Core
{
    void ToastHelper::ShowToast(std::wstring const& title, std::wstring const& message)
    {
        try
        {
            auto builder = AppNotificationBuilder()
                .AddText(winrt::hstring(title))
                .AddText(winrt::hstring(message));

            auto notification = builder.BuildNotification();
            AppNotificationManager::Default().Show(notification);
        }
        catch (winrt::hresult_error const&)
        {
            // Silently fail if notifications not available
        }
    }

    void ToastHelper::ShowUploadSuccess(std::wstring const& fileName, std::wstring const& /*url*/)
    {
        try
        {
            auto builder = AppNotificationBuilder()
                .AddText(winrt::hstring(L"Upload Complete"))
                .AddText(winrt::hstring(fileName + L" - Link copied to clipboard"));

            auto notification = builder.BuildNotification();
            AppNotificationManager::Default().Show(notification);
        }
        catch (winrt::hresult_error const&)
        {
            // Silently fail
        }
    }

    void ToastHelper::ShowUploadError(std::wstring const& fileName, std::wstring const& error)
    {
        try
        {
            auto builder = AppNotificationBuilder()
                .AddText(winrt::hstring(L"Upload Failed"))
                .AddText(winrt::hstring(fileName + L": " + error));

            auto notification = builder.BuildNotification();
            AppNotificationManager::Default().Show(notification);
        }
        catch (winrt::hresult_error const&)
        {
            // Silently fail
        }
    }

    void ToastHelper::ShowInfo(std::wstring const& message)
    {
        try
        {
            auto builder = AppNotificationBuilder()
                .AddText(winrt::hstring(L"Fling"))
                .AddText(winrt::hstring(message));

            auto notification = builder.BuildNotification();
            AppNotificationManager::Default().Show(notification);
        }
        catch (winrt::hresult_error const&)
        {
            // Silently fail
        }
    }
}
