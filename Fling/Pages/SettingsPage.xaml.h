#pragma once
#include "SettingsPage.g.h"
#include "Models/FlingConfig.h"
#include <atomic>

namespace winrt::Fling::implementation
{
    struct SettingsPage : SettingsPageT<SettingsPage>
    {
        SettingsPage();

        void OnNavigatedTo(winrt::Microsoft::UI::Xaml::Navigation::NavigationEventArgs const& e);

        void SaveButton_Click(winrt::Windows::Foundation::IInspectable const& sender,
                              winrt::Microsoft::UI::Xaml::RoutedEventArgs const& e);
        winrt::fire_and_forget TestButton_Click(winrt::Windows::Foundation::IInspectable const& sender,
                                                winrt::Microsoft::UI::Xaml::RoutedEventArgs const& e);
        void RevealButton_Click(winrt::Windows::Foundation::IInspectable const& sender,
                                winrt::Microsoft::UI::Xaml::RoutedEventArgs const& e);
        void CopySecretButton_Click(winrt::Windows::Foundation::IInspectable const& sender,
                                    winrt::Microsoft::UI::Xaml::RoutedEventArgs const& e);
        void OpenLogFolder_Click(winrt::Windows::Foundation::IInspectable const& sender,
                                 winrt::Microsoft::UI::Xaml::RoutedEventArgs const& e);
        void CopyDiagnostics_Click(winrt::Windows::Foundation::IInspectable const& sender,
                                   winrt::Microsoft::UI::Xaml::RoutedEventArgs const& e);
        void StorageChoice_SelectionChanged(winrt::Windows::Foundation::IInspectable const& sender,
                                            winrt::Microsoft::UI::Xaml::Controls::SelectionChangedEventArgs const& e);
        void GatewayUrlTextBox_TextChanged(winrt::Windows::Foundation::IInspectable const& sender,
                                           winrt::Microsoft::UI::Xaml::Controls::TextChangedEventArgs const& e);
        winrt::fire_and_forget GatewaySignIn_Click(winrt::Windows::Foundation::IInspectable const& sender,
                                                   winrt::Microsoft::UI::Xaml::RoutedEventArgs const& e);
        void GatewayCancel_Click(winrt::Windows::Foundation::IInspectable const& sender,
                                 winrt::Microsoft::UI::Xaml::RoutedEventArgs const& e);
        winrt::fire_and_forget GatewaySignOut_Click(winrt::Windows::Foundation::IInspectable const& sender,
                                                    winrt::Microsoft::UI::Xaml::RoutedEventArgs const& e);

    private:
        enum class Status { Busy, Ok, Problem };

        void LoadConfig();
        ::Fling::Models::FlingConfig ReadForm();
        void ShowStatus(Status status, winrt::hstring const& message);
        bool UsesGatewayChoice();
        void UpdateStorageCards();
        void RefreshGatewayAccount();

        std::atomic<bool> m_cancelSignIn{ false };
        bool m_signingIn{ false };
    };
}

namespace winrt::Fling::factory_implementation
{
    struct SettingsPage : SettingsPageT<SettingsPage, implementation::SettingsPage>
    {
    };
}
