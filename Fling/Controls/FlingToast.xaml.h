#pragma once
#include "FlingToast.g.h"

#include <string>

namespace winrt::Fling::implementation
{
    enum class ToastTone { Neutral, Done, Alert };

    // In-window floating message (Design/Components/Toast.md). One at a time; alerts stay until dismissed.
    struct FlingToast : FlingToastT<FlingToast>
    {
        FlingToast();

        void Show(ToastTone tone, std::wstring const& message);

        void Dismiss_Click(winrt::Windows::Foundation::IInspectable const&, winrt::Microsoft::UI::Xaml::RoutedEventArgs const&);

    private:
        winrt::Microsoft::UI::Dispatching::DispatcherQueueTimer m_timer{ nullptr };
    };
}

namespace winrt::Fling::factory_implementation
{
    struct FlingToast : FlingToastT<FlingToast, implementation::FlingToast>
    {
    };
}
