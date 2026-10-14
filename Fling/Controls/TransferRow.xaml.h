#pragma once
#include "TransferRow.g.h"

#include <functional>
#include <string>

namespace winrt::Fling::implementation
{
    // One file's upload status (Design/Components/TransferRow.md), driven from code.
    struct TransferRow : TransferRowT<TransferRow>
    {
        TransferRow();

        void SetFile(std::wstring const& name, std::wstring const& target);
        void ShowWaiting(std::wstring const& detail);
        void ShowSending(double fraction, std::wstring const& detail);
        // actionLabel empty = no button. Done/inactive actions are quiet; failure's is primary.
        void ShowDone(std::wstring const& detail, std::wstring const& actionLabel = {});
        void ShowFailed(std::wstring const& reason, std::wstring const& actionLabel = L"Try again");
        void ShowInactive(std::wstring const& detail);

        std::function<void()> ActionInvoked;

        void ActionButton_Click(winrt::Windows::Foundation::IInspectable const&, winrt::Microsoft::UI::Xaml::RoutedEventArgs const&);

    private:
        void SetAction(std::wstring const& label, wchar_t const* styleKey);
    };
}

namespace winrt::Fling::factory_implementation
{
    struct TransferRow : TransferRowT<TransferRow, implementation::TransferRow>
    {
    };
}
