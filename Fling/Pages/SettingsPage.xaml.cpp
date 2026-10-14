#include "pch.h"
#include "SettingsPage.xaml.h"

#if __has_include("SettingsPage.g.cpp")
#include "SettingsPage.g.cpp"
#endif

#include "Controls/FlingToast.xaml.h"
#include "Core/ClipboardHelper.h"
#include "Core/ConfigManager.h"
#include "Core/S3Uploader.h"

#include <cmath>

using namespace winrt;
using namespace winrt::Microsoft::UI::Xaml;
using namespace winrt::Microsoft::UI::Xaml::Controls;
using namespace winrt::Microsoft::UI::Xaml::Navigation;
using namespace ::Fling::Core;
using namespace ::Fling::Models;

namespace
{
    constexpr int kSecondsPerDay = 86400;
}

namespace winrt::Fling::implementation
{
    SettingsPage::SettingsPage()
    {
        InitializeComponent();
    }

    void SettingsPage::OnNavigatedTo(NavigationEventArgs const&)
    {
        LoadConfig();
    }

    void SettingsPage::LoadConfig()
    {
        auto config = ConfigManager().Load();

        EndpointTextBox().Text(hstring(config.endpoint));
        RegionTextBox().Text(hstring(config.region));
        BucketTextBox().Text(hstring(config.bucket));
        AccessKeyTextBox().Text(hstring(config.accessKey));
        SecretKeyPasswordBox().Password(hstring(config.secretKey));
        ExpiryNumberBox().Value(std::clamp(std::round(config.expirySeconds / double(kSecondsPerDay)), 1.0, 7.0));
        PathStyleCheckBox().IsChecked(config.usePathStyle);
    }

    FlingConfig SettingsPage::ReadForm()
    {
        FlingConfig config;
        config.endpoint = std::wstring(EndpointTextBox().Text());
        config.region = std::wstring(RegionTextBox().Text());
        config.bucket = std::wstring(BucketTextBox().Text());
        config.accessKey = std::wstring(AccessKeyTextBox().Text());
        config.secretKey = std::wstring(SecretKeyPasswordBox().Password());

        double days = ExpiryNumberBox().Value();
        if (std::isnan(days))
        {
            days = 7;
        }
        config.expirySeconds = static_cast<int>(std::clamp(days, 1.0, 7.0)) * kSecondsPerDay;

        auto checked = PathStyleCheckBox().IsChecked();
        config.usePathStyle = checked && checked.Value();
        return config;
    }

    void SettingsPage::SaveButton_Click(IInspectable const&, RoutedEventArgs const&)
    {
        auto config = ReadForm();
        if (!config.IsConfigured())
        {
            ShowStatus(Status::Problem, L"Fill in the endpoint, bucket, access key and secret key.");
            return;
        }

        ConfigManager cm;
        // keyPrefix isn't in the form; keep what's on disk.
        config.keyPrefix = cm.Load().keyPrefix;
        if (cm.Save(config))
        {
            ShowStatus(Status::Ok, L"Saved.");
        }
        else
        {
            ShowStatus(Status::Problem, L"Couldn't save the settings file.");
        }
    }

    fire_and_forget SettingsPage::TestButton_Click(IInspectable const&, RoutedEventArgs const&)
    {
        auto strongThis = get_strong();
        auto config = ReadForm();
        if (!config.IsConfigured())
        {
            ShowStatus(Status::Problem, L"Fill in the endpoint, bucket, access key and secret key first.");
            co_return;
        }

        TestButton().IsEnabled(false);
        ShowStatus(Status::Busy, L"Testing the connection\u2026");

        apartment_context uiThread;
        co_await resume_background();
        auto result = S3Uploader(config).TestConnection();
        co_await uiThread;

        strongThis->TestButton().IsEnabled(true);
        if (result.success)
        {
            strongThis->ShowStatus(Status::Ok, hstring(L"Connected to " + config.bucket + L"."));
        }
        else
        {
            strongThis->ShowStatus(Status::Problem, hstring(L"Couldn't connect. " + result.errorMessage));
        }
    }

    void SettingsPage::RevealButton_Click(IInspectable const&, RoutedEventArgs const&)
    {
        bool reveal = SecretKeyPasswordBox().PasswordRevealMode() != PasswordRevealMode::Visible;
        SecretKeyPasswordBox().PasswordRevealMode(reveal ? PasswordRevealMode::Visible : PasswordRevealMode::Hidden);
        RevealButton().Content(box_value(reveal ? L"Hide" : L"Show"));
    }

    void SettingsPage::CopySecretButton_Click(IInspectable const&, RoutedEventArgs const&)
    {
        auto secret = std::wstring(SecretKeyPasswordBox().Password());
        auto toast = get_self<FlingToast>(Toast());
        if (secret.empty())
        {
            toast->Show(ToastTone::Alert, L"There's no secret key to copy");
        }
        else if (ClipboardHelper::CopyToClipboard(secret))
        {
            toast->Show(ToastTone::Neutral, L"Secret key copied");
        }
        else
        {
            toast->Show(ToastTone::Alert, L"Couldn't copy the secret key");
        }
    }

    void SettingsPage::ShowStatus(Status status, hstring const& message)
    {
        auto resources = Application::Current().Resources();
        auto glyph = StatusGlyph();
        switch (status)
        {
        case Status::Busy:
            glyph.Glyph(L"\uE895");
            glyph.Foreground(resources.Lookup(box_value(L"FlingInkMutedBrush")).as<Media::Brush>());
            break;
        case Status::Ok:
            glyph.Glyph(L"\uE73E");
            glyph.Foreground(resources.Lookup(box_value(L"FlingDoneBrush")).as<Media::Brush>());
            break;
        case Status::Problem:
            glyph.Glyph(L"\uE7BA");
            glyph.Foreground(resources.Lookup(box_value(L"FlingInkBrush")).as<Media::Brush>());
            break;
        }
        StatusText().Text(message);
        StatusPanel().Visibility(Visibility::Visible);
    }
}
