#pragma once
#include <string>

namespace Fling::Core
{
    class ToastHelper
    {
    public:
        // Show a simple toast notification
        static void ShowToast(std::wstring const& title, std::wstring const& message);

        // Show upload success toast with action button
        static void ShowUploadSuccess(std::wstring const& fileName, std::wstring const& url);

        // Show upload error toast
        static void ShowUploadError(std::wstring const& fileName, std::wstring const& error);

        // Show info toast
        static void ShowInfo(std::wstring const& message);
    };
}
