#pragma once
#include "HistoryPage.g.h"
#include "Models/UploadEntry.h"

namespace winrt::Fling::implementation
{
    // "Uploads" page: drop zone plus recent uploads.
    struct HistoryPage : HistoryPageT<HistoryPage>
    {
        HistoryPage();

        void OnNavigatedTo(winrt::Microsoft::UI::Xaml::Navigation::NavigationEventArgs const& e);
        void OnNavigatedFrom(winrt::Microsoft::UI::Xaml::Navigation::NavigationEventArgs const& e);

        winrt::fire_and_forget ClearButton_Click(
            winrt::Windows::Foundation::IInspectable const& sender,
            winrt::Microsoft::UI::Xaml::RoutedEventArgs const& e);

        // Reloads the list if the page is currently shown (called when uploads finish elsewhere).
        static void RefreshVisible();

    private:
        void LoadHistory();
        winrt::Fling::TransferRow BuildRow(::Fling::Models::UploadEntry const& entry);

        static winrt::weak_ref<HistoryPage> s_visible;
    };
}

namespace winrt::Fling::factory_implementation
{
    struct HistoryPage : HistoryPageT<HistoryPage, implementation::HistoryPage>
    {
    };
}
