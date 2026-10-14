#include "pch.h"
#include "SettingsPage.xaml.h"

#if __has_include("SettingsPage.g.cpp")
#include "SettingsPage.g.cpp"
#endif

#include "Core/ConfigManager.h"
#include "Core/S3Uploader.h"
#include "Models/FlingConfig.h"

#include <winrt/Microsoft.UI.Xaml.Controls.h>

using namespace winrt;
using namespace winrt::Microsoft::UI::Xaml;
using namespace winrt::Microsoft::UI::Xaml::Controls;
using namespace winrt::Microsoft::UI::Xaml::Navigation;
using namespace ::Fling::Core;
using namespace ::Fling::Models;

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
        ConfigManager cm;
        auto config = cm.Load();

        EndpointTextBox().Text(hstring(config.endpoint));
        RegionTextBox().Text(hstring(config.region));
        BucketTextBox().Text(hstring(config.bucket));
        AccessKeyTextBox().Text(hstring(config.accessKey));
        SecretKeyPasswordBox().Password(hstring(config.secretKey));
        ExpiryNumberBox().Value(static_cast<double>(config.expirySeconds));
        PathStyleCheckBox().IsChecked(config.usePathStyle);
    }

    void SettingsPage::SaveConfig()
    {
        FlingConfig config;
        config.endpoint = std::wstring(EndpointTextBox().Text());
        config.region = std::wstring(RegionTextBox().Text());
        config.bucket = std::wstring(BucketTextBox().Text());
        config.accessKey = std::wstring(AccessKeyTextBox().Text());
        config.secretKey = std::wstring(SecretKeyPasswordBox().Password());

        auto expiry = ExpiryNumberBox().Value();
        if (std::isnan(expiry) || expiry <= 0)
        {
            expiry = 604800;
        }
        config.expirySeconds = static_cast<int>(expiry);

        auto checked = PathStyleCheckBox().IsChecked();
        config.usePathStyle = checked && checked.Value();

        // Preserve keyPrefix — not exposed in this UI yet, carry over what's on disk.
        ConfigManager cm;
        auto existing = cm.Load();
        config.keyPrefix = existing.keyPrefix;

        if (!config.IsConfigured())
        {
            ShowStatus(false, L"Bucket, access key, and secret key are required.");
            return;
        }

        if (cm.Save(config))
        {
            ShowStatus(true, L"Settings saved.");
        }
        else
        {
            ShowStatus(false, L"Failed to write config file.");
        }
    }

    void SettingsPage::SaveButton_Click(IInspectable const&, RoutedEventArgs const&)
    {
        SaveConfig();
    }

    void SettingsPage::TestButton_Click(IInspectable const&, RoutedEventArgs const&)
    {
        TestConnection();
    }

    void SettingsPage::TestConnection()
    {
        FlingConfig config;
        config.endpoint = std::wstring(EndpointTextBox().Text());
        config.region = std::wstring(RegionTextBox().Text());
        config.bucket = std::wstring(BucketTextBox().Text());
        config.accessKey = std::wstring(AccessKeyTextBox().Text());
        config.secretKey = std::wstring(SecretKeyPasswordBox().Password());
        auto checked = PathStyleCheckBox().IsChecked();
        config.usePathStyle = checked && checked.Value();

        if (!config.IsConfigured())
        {
            ShowStatus(false, L"Fill in bucket and credentials before testing.");
            return;
        }

        ShowStatus(true, L"Testing connection...");

        S3Uploader uploader(config);
        auto result = uploader.TestConnection();

        if (result.success)
        {
            ShowStatus(true, L"Connected successfully.");
        }
        else
        {
            ShowStatus(false, hstring(L"Connection failed: " + result.errorMessage));
        }
    }

    void SettingsPage::ShowStatus(bool success, hstring const& message)
    {
        StatusInfoBar().Message(message);
        StatusInfoBar().Severity(success
            ? InfoBarSeverity::Success
            : InfoBarSeverity::Error);
        StatusInfoBar().IsOpen(true);
    }
}
