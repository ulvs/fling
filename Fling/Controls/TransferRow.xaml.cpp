#include "pch.h"
#include "TransferRow.xaml.h"

#if __has_include("TransferRow.g.cpp")
#include "TransferRow.g.cpp"
#endif

#include <winrt/Microsoft.UI.Xaml.Automation.h>

using namespace winrt;
using namespace winrt::Microsoft::UI::Xaml;
using namespace winrt::Microsoft::UI::Xaml::Controls;

namespace winrt::Fling::implementation
{
    TransferRow::TransferRow()
    {
        InitializeComponent();
    }

    void TransferRow::SetFile(std::wstring const& name, std::wstring const& target)
    {
        NameText().Text(hstring(name));
        TargetText().Text(target.empty() ? hstring() : hstring(L"\u2192 " + target));
        Automation::AutomationProperties::SetName(*this, hstring(name));
    }

    void TransferRow::ShowWaiting(std::wstring const& detail)
    {
        DetailText().Text(hstring(detail));
        SetAction({}, nullptr);
        VisualStateManager::GoToState(*this, L"Waiting", false);
    }

    void TransferRow::ShowSending(double fraction, std::wstring const& detail)
    {
        Progress().Value(fraction);
        DetailText().Text(hstring(detail));
        SetAction({}, nullptr);
        VisualStateManager::GoToState(*this, L"Sending", false);
    }

    void TransferRow::ShowDone(std::wstring const& detail, std::wstring const& actionLabel)
    {
        DetailText().Text(hstring(detail));
        SetAction(actionLabel, L"FlingQuietSmallButtonStyle");
        VisualStateManager::GoToState(*this, L"Done", false);
    }

    void TransferRow::ShowFailed(std::wstring const& reason, std::wstring const& actionLabel)
    {
        ReasonText().Text(hstring(reason));
        SetAction(actionLabel, L"FlingPrimarySmallButtonStyle");
        VisualStateManager::GoToState(*this, L"Failed", false);
    }

    void TransferRow::ShowInactive(std::wstring const& detail)
    {
        DetailText().Text(hstring(detail));
        SetAction({}, nullptr);
        VisualStateManager::GoToState(*this, L"Inactive", false);
    }

    void TransferRow::SetAction(std::wstring const& label, wchar_t const* styleKey)
    {
        auto button = ActionButton();
        if (label.empty())
        {
            button.Visibility(Visibility::Collapsed);
            return;
        }
        button.Style(Application::Current().Resources().Lookup(box_value(styleKey)).as<winrt::Microsoft::UI::Xaml::Style>());
        button.Content(box_value(label));
        button.Visibility(Visibility::Visible);
    }

    void TransferRow::ActionButton_Click(IInspectable const&, RoutedEventArgs const&)
    {
        if (ActionInvoked)
        {
            ActionInvoked();
        }
    }
}
