#include "pch.h"
#include "SettingsPage.xaml.h"

#if __has_include("SettingsPage.g.cpp")
#include "SettingsPage.g.cpp"
#endif

#include "Controls/FlingToast.xaml.h"
#include "Core/ClipboardHelper.h"
#include "Core/ConfigManager.h"
#include "Core/GatewayClient.h"
#include "Core/GatewaySignIn.h"
#include "Core/Log.h"
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
        MultipleChoice().SelectedIndex(config.zipMultiple ? 0 : 1);
        GatewayUrlTextBox().Text(hstring(config.gatewayUrl));
        StorageChoice().SelectedIndex(config.UsesGateway() ? 1 : 0);
        UpdateStorageCards();
        RefreshGatewayAccount();
    }

    bool SettingsPage::UsesGatewayChoice()
    {
        return StorageChoice().SelectedIndex() == 1;
    }

    void SettingsPage::UpdateStorageCards()
    {
        bool gateway = UsesGatewayChoice();
        GatewayCard().Visibility(gateway ? Visibility::Visible : Visibility::Collapsed);
        StorageCard().Visibility(gateway ? Visibility::Collapsed : Visibility::Visible);
    }

    // Shows who's signed in to the gateway in the address box (tokens are kept per address).
    void SettingsPage::RefreshGatewayAccount()
    {
        if (m_signingIn)
        {
            return;
        }
        auto url = GatewayClient::NormalizeUrl(std::wstring(GatewayUrlTextBox().Text()));
        auto stored = url.empty() ? std::nullopt : GatewayCredentials::Load(url);
        GatewayAccountText().Text(stored ? hstring(L"Signed in as " + stored->email + L".") : hstring(L"Not signed in."));
        GatewaySignInButton().Content(box_value(stored ? L"Sign in again" : L"Sign in"));
        GatewaySignOutButton().Visibility(stored ? Visibility::Visible : Visibility::Collapsed);
    }

    void SettingsPage::StorageChoice_SelectionChanged(IInspectable const&, SelectionChangedEventArgs const&)
    {
        UpdateStorageCards();
    }

    void SettingsPage::GatewayUrlTextBox_TextChanged(IInspectable const&, TextChangedEventArgs const&)
    {
        RefreshGatewayAccount();
    }

    fire_and_forget SettingsPage::GatewaySignIn_Click(IInspectable const&, RoutedEventArgs const&)
    {
        auto strongThis = get_strong();
        auto url = GatewayClient::NormalizeUrl(std::wstring(GatewayUrlTextBox().Text()));
        if (url.empty())
        {
            ShowStatus(Status::Problem, L"Enter the gateway's address, like https://fling.example.com.");
            co_return;
        }
        GatewayUrlTextBox().Text(hstring(url));

        // Save the choice now, so shares use the gateway as soon as sign-in finishes.
        ConfigManager cm;
        auto config = cm.Load();
        config.storage = L"gateway";
        config.gatewayUrl = url;
        cm.Save(config);

        m_signingIn = true;
        m_cancelSignIn = false;
        GatewaySignInButton().IsEnabled(false);
        GatewayCancelButton().Visibility(Visibility::Visible);
        GatewaySignOutButton().Visibility(Visibility::Collapsed);
        GatewayAccountText().Text(L"Finish signing in in your browser\u2026");
        StatusPanel().Visibility(Visibility::Collapsed);

        apartment_context uiThread;
        co_await resume_background();
        auto result = GatewaySignIn::SignIn(url, strongThis->m_cancelSignIn);
        if (result.success)
        {
            GatewayCredentials::Save(url, result.email, result.token);
        }
        co_await uiThread;

        strongThis->m_signingIn = false;
        strongThis->GatewaySignInButton().IsEnabled(true);
        strongThis->GatewayCancelButton().Visibility(Visibility::Collapsed);
        strongThis->RefreshGatewayAccount();
        auto toast = get_self<FlingToast>(strongThis->Toast());
        if (result.success)
        {
            toast->Show(ToastTone::Neutral, L"Signed in as " + result.email);
        }
        else if (!strongThis->m_cancelSignIn)
        {
            strongThis->ShowStatus(Status::Problem, hstring(L"Couldn't sign in. " + result.errorMessage));
        }
    }

    void SettingsPage::GatewayCancel_Click(IInspectable const&, RoutedEventArgs const&)
    {
        m_cancelSignIn = true;
    }

    fire_and_forget SettingsPage::GatewaySignOut_Click(IInspectable const&, RoutedEventArgs const&)
    {
        auto strongThis = get_strong();
        auto url = GatewayClient::NormalizeUrl(std::wstring(GatewayUrlTextBox().Text()));
        auto stored = url.empty() ? std::nullopt : GatewayCredentials::Load(url);
        GatewayCredentials::Remove(url);
        RefreshGatewayAccount();
        get_self<FlingToast>(Toast())->Show(ToastTone::Neutral, L"Signed out");
        if (stored)
        {
            // Tell the gateway too, so the token stops working everywhere. Best effort.
            co_await resume_background();
            GatewayClient(url, stored->token).SignOut();
        }
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
        config.zipMultiple = MultipleChoice().SelectedIndex() != 1;
        config.storage = UsesGatewayChoice() ? L"gateway" : L"s3";
        config.gatewayUrl = GatewayClient::NormalizeUrl(std::wstring(GatewayUrlTextBox().Text()));
        return config;
    }

    void SettingsPage::SaveButton_Click(IInspectable const&, RoutedEventArgs const&)
    {
        auto config = ReadForm();
        if (config.UsesGateway() && config.gatewayUrl.empty())
        {
            ShowStatus(Status::Problem, L"Enter the gateway's address, like https://fling.example.com.");
            return;
        }
        if (!config.UsesGateway() && !config.IsS3Configured())
        {
            ShowStatus(Status::Problem, L"Fill in the endpoint, bucket, access key and secret key.");
            return;
        }
        GatewayUrlTextBox().Text(hstring(config.gatewayUrl));

        ConfigManager cm;
        // keyPrefix isn't in the form; keep what's on disk.
        config.keyPrefix = cm.Load().keyPrefix;
        if (cm.Save(config))
        {
            ShowStatus(Status::Ok, L"Saved.");
        }
        else
        {
            Log::Error(L"Couldn't save settings");
            ShowStatus(Status::Problem, L"Couldn't save the settings file.");
        }
    }

    fire_and_forget SettingsPage::TestButton_Click(IInspectable const&, RoutedEventArgs const&)
    {
        auto strongThis = get_strong();
        auto config = ReadForm();
        if (config.UsesGateway())
        {
            auto stored = config.gatewayUrl.empty() ? std::nullopt : GatewayCredentials::Load(config.gatewayUrl);
            if (!stored)
            {
                ShowStatus(Status::Problem, L"Sign in to the gateway first.");
                co_return;
            }
            TestButton().IsEnabled(false);
            ShowStatus(Status::Busy, L"Testing the connection\u2026");
            apartment_context ui;
            co_await resume_background();
            auto me = GatewayClient(config.gatewayUrl, stored->token).Me();
            if (me.signInNeeded)
            {
                GatewayCredentials::Remove(config.gatewayUrl);
            }
            co_await ui;
            strongThis->TestButton().IsEnabled(true);
            strongThis->RefreshGatewayAccount();
            if (me.success)
            {
                Log::Info(L"Gateway connection test succeeded");
                strongThis->ShowStatus(Status::Ok, hstring(L"Connected to the gateway as " + me.email + L"."));
            }
            else
            {
                Log::Warning(L"Gateway connection test failed: " + me.errorMessage);
                strongThis->ShowStatus(Status::Problem, hstring(me.signInNeeded
                    ? std::wstring(L"Your sign-in has ended. Sign in again.")
                    : L"Couldn't connect. " + me.errorMessage));
            }
            co_return;
        }
        if (!config.IsS3Configured())
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
            Log::Info(L"Connection test succeeded");
            strongThis->ShowStatus(Status::Ok, hstring(L"Connected to " + config.bucket + L"."));
        }
        else
        {
            Log::Warning(L"Connection test failed: " + result.errorMessage);
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

    void SettingsPage::OpenLogFolder_Click(IInspectable const&, RoutedEventArgs const&)
    {
        std::error_code ec;
        auto folder = Log::Folder();
        std::filesystem::create_directories(folder, ec);
        ShellExecuteW(nullptr, L"open", folder.c_str(), nullptr, nullptr, SW_SHOWNORMAL);
    }

    void SettingsPage::CopyDiagnostics_Click(IInspectable const&, RoutedEventArgs const&)
    {
        bool copied = ClipboardHelper::CopyToClipboard(Log::DiagnosticInfo());
        get_self<FlingToast>(Toast())->Show(copied ? ToastTone::Neutral : ToastTone::Alert,
            copied ? L"Diagnostic info copied" : L"Couldn't copy the diagnostic info");
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
