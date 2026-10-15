#pragma once
#include "SettingsPage.g.h"
#include "Models/FlingConfig.h"

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

    private:
        enum class Status { Busy, Ok, Problem };

        void LoadConfig();
        ::Fling::Models::FlingConfig ReadForm();
        void ShowStatus(Status status, winrt::hstring const& message);
    };
}

namespace winrt::Fling::factory_implementation
{
    struct SettingsPage : SettingsPageT<SettingsPage, implementation::SettingsPage>
    {
    };
}
