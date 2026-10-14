#pragma once
#include "SettingsPage.g.h"

namespace winrt::Fling::implementation
{
    struct SettingsPage : SettingsPageT<SettingsPage>
    {
        SettingsPage();

        void OnNavigatedTo(winrt::Microsoft::UI::Xaml::Navigation::NavigationEventArgs const& e);

        void SaveButton_Click(winrt::Windows::Foundation::IInspectable const& sender,
                              winrt::Microsoft::UI::Xaml::RoutedEventArgs const& e);

        void TestButton_Click(winrt::Windows::Foundation::IInspectable const& sender,
                              winrt::Microsoft::UI::Xaml::RoutedEventArgs const& e);


    private:
        void LoadConfig();
        void SaveConfig();
        void TestConnection();
        void ShowStatus(bool success, winrt::hstring const& message);
    };
}

namespace winrt::Fling::factory_implementation
{
    struct SettingsPage : SettingsPageT<SettingsPage, implementation::SettingsPage>
    {
    };
}
