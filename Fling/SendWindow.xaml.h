#pragma once
#include "SendWindow.g.h"

#include <filesystem>
#include <string>
#include <vector>

namespace winrt::Fling::implementation
{
    // Compact always-on-top window shown for "Fling it" launches and drops:
    // one TransferRow per upload (a file, or a zip of several items).
    struct SendWindow : SendWindowT<SendWindow>
    {
        SendWindow();

        void Start(std::vector<std::filesystem::path> items);

        void InfoButton_Click(winrt::Windows::Foundation::IInspectable const&, winrt::Microsoft::UI::Xaml::RoutedEventArgs const&);
        void SettingsButton_Click(winrt::Windows::Foundation::IInspectable const&, winrt::Microsoft::UI::Xaml::RoutedEventArgs const&);
        void CopyButton_Click(winrt::Windows::Foundation::IInspectable const&, winrt::Microsoft::UI::Xaml::RoutedEventArgs const&);
        void CloseButton_Click(winrt::Windows::Foundation::IInspectable const&, winrt::Microsoft::UI::Xaml::RoutedEventArgs const&);
        void Root_PointerEntered(winrt::Windows::Foundation::IInspectable const&, winrt::Microsoft::UI::Xaml::Input::PointerRoutedEventArgs const&);

    private:
        struct Job
        {
            std::wstring name;
            std::filesystem::path path;                  // the file to upload (empty for archives until zipped)
            bool archive{ false };
            std::vector<std::filesystem::path> sources;  // what goes into the zip
            uint64_t size{ 0 };
            winrt::Fling::TransferRow row{ nullptr };
            bool succeeded{ false };
            std::wstring url;
            std::wstring error;
        };

        winrt::Windows::Foundation::IAsyncAction RunJob(size_t index);
        winrt::fire_and_forget RunAll();
        winrt::fire_and_forget Retry(size_t index);
        void Finish();
        void PlaceInCorner(size_t rowCount);

        std::vector<Job> m_jobs;
        int m_active{ 0 };
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
