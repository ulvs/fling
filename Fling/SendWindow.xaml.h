#pragma once
#include "SendWindow.g.h"

#include <filesystem>
#include <string>
#include <vector>

namespace winrt::Fling::implementation
{
    // Compact always-on-top window shown for Explorer "Share with Fling" launches.
    struct SendWindow : SendWindowT<SendWindow>
    {
        SendWindow();

        void Start(std::vector<std::filesystem::path> files);

        void InfoButton_Click(winrt::Windows::Foundation::IInspectable const&, winrt::Microsoft::UI::Xaml::RoutedEventArgs const&);
        void SettingsButton_Click(winrt::Windows::Foundation::IInspectable const&, winrt::Microsoft::UI::Xaml::RoutedEventArgs const&);
        void CopyButton_Click(winrt::Windows::Foundation::IInspectable const&, winrt::Microsoft::UI::Xaml::RoutedEventArgs const&);
        void CloseButton_Click(winrt::Windows::Foundation::IInspectable const&, winrt::Microsoft::UI::Xaml::RoutedEventArgs const&);
        void Root_PointerEntered(winrt::Windows::Foundation::IInspectable const&, winrt::Microsoft::UI::Xaml::Input::PointerRoutedEventArgs const&);

    private:
        winrt::fire_and_forget RunUploads(std::vector<std::filesystem::path> files);
        void PlaceInCorner();
        void SetProgress(double fraction);
        void ShowResult(size_t total, std::vector<std::wstring> const& urls, std::vector<std::wstring> const& errors);

        bool m_uploading{ false };
        bool m_dismissed{ false };
        std::wstring m_links;
        winrt::Microsoft::UI::Dispatching::DispatcherQueueTimer m_autoCloseTimer{ nullptr };
    };
}

namespace winrt::Fling::factory_implementation
{
    struct SendWindow : SendWindowT<SendWindow, implementation::SendWindow>
    {
    };
}
