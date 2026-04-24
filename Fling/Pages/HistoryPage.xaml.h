#pragma once
#include "HistoryPage.g.h"
#include "Models/UploadEntry.h"
#include <winrt/Windows.Foundation.Collections.h>

namespace winrt::Fling::implementation
{
    struct HistoryPage : HistoryPageT<HistoryPage>
    {
        HistoryPage();

        void OnNavigatedTo(winrt::Microsoft::UI::Xaml::Navigation::NavigationEventArgs const& e);

        void RefreshButton_Click(winrt::Windows::Foundation::IInspectable const& sender,
                                 winrt::Microsoft::UI::Xaml::RoutedEventArgs const& e);

        winrt::fire_and_forget ClearAllButton_Click(
            winrt::Windows::Foundation::IInspectable const& sender,
            winrt::Microsoft::UI::Xaml::RoutedEventArgs const& e);

        winrt::fire_and_forget ShareFileButton_Click(
            winrt::Windows::Foundation::IInspectable const& sender,
            winrt::Microsoft::UI::Xaml::RoutedEventArgs const& e);

        void CopyLinkButton_Click(winrt::Windows::Foundation::IInspectable const& sender,
                                  winrt::Microsoft::UI::Xaml::RoutedEventArgs const& e);

    private:
        void LoadHistory();
        void UpdateEmptyState(bool empty);
        winrt::Windows::Foundation::IInspectable BuildEntryItem(
            ::Fling::Models::UploadEntry const& entry);
    };
}

namespace winrt::Fling::factory_implementation
{
    struct HistoryPage : HistoryPageT<HistoryPage, implementation::HistoryPage>
    {
    };
}
