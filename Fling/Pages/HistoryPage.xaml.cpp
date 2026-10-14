#include "pch.h"
#include "HistoryPage.xaml.h"

#if __has_include("HistoryPage.g.cpp")
#include "HistoryPage.g.cpp"
#endif

#include "App.xaml.h"
#include "Core/HistoryManager.h"
#include "Core/ClipboardHelper.h"
#include "Core/ToastHelper.h"
#include "Core/UploadCoordinator.h"

#include <winrt/Microsoft.UI.Xaml.Controls.h>
#include <winrt/Microsoft.UI.Xaml.Media.h>
#include <winrt/Windows.Storage.h>
#include <winrt/Windows.Storage.Pickers.h>
#include <winrt/Windows.UI.h>
#include <winrt/Windows.UI.Text.h>

// IInitializeWithWindow lives in <shobjidl.h>, but including that header here
// clashes with the CppWinRT-flavored IUnknown brought in by the WinRT headers.
// The interface is tiny, so we declare what we need directly.
struct __declspec(uuid("3E68D4BD-7135-4D10-8018-9FB6D9F33FA1")) IInitializeWithWindow : ::IUnknown
{
    virtual HRESULT __stdcall Initialize(HWND hwnd) = 0;
};

#include <chrono>
#include <ctime>
#include <sstream>
#include <iomanip>

using namespace winrt;
using namespace winrt::Windows::Foundation;
using namespace winrt::Windows::Foundation::Collections;
using namespace winrt::Microsoft::UI::Xaml;
using namespace winrt::Microsoft::UI::Xaml::Controls;
using namespace winrt::Microsoft::UI::Xaml::Media;
using namespace winrt::Microsoft::UI::Xaml::Navigation;
using namespace ::Fling::Core;

namespace
{
    std::wstring FormatSize(uint64_t bytes)
    {
        constexpr double kb = 1024.0;
        constexpr double mb = kb * 1024.0;
        constexpr double gb = mb * 1024.0;

        std::wostringstream os;
        os << std::fixed << std::setprecision(1);
        if (bytes >= gb)       os << (bytes / gb) << L" GB";
        else if (bytes >= mb)  os << (bytes / mb) << L" MB";
        else if (bytes >= kb)  os << (bytes / kb) << L" KB";
        else                   os << bytes << L" B";
        return os.str();
    }

    std::wstring FormatDate(int64_t epochSeconds)
    {
        if (epochSeconds <= 0) return L"—";
        std::time_t t = static_cast<std::time_t>(epochSeconds);
        std::tm tm{};
        localtime_s(&tm, &t);
        std::wostringstream os;
        os << std::put_time(&tm, L"%Y-%m-%d %H:%M");
        return os.str();
    }

    std::wstring ExpiryStatus(::Fling::Models::UploadEntry const& e)
    {
        if (!e.success)
        {
            return L"Failed";
        }
        if (e.IsExpired())
        {
            return L"Expired";
        }
        using namespace std::chrono;
        auto now = duration_cast<seconds>(system_clock::now().time_since_epoch()).count();
        auto remaining = e.expiresAt - now;

        auto days = remaining / 86400;
        auto hours = (remaining % 86400) / 3600;

        std::wostringstream os;
        if (days > 0)       os << days << L"d " << hours << L"h left";
        else if (hours > 0) os << hours << L"h left";
        else                os << (remaining / 60) << L"m left";
        return os.str();
    }
}

namespace winrt::Fling::implementation
{
    HistoryPage::HistoryPage()
    {
        InitializeComponent();
    }

    void HistoryPage::OnNavigatedTo(NavigationEventArgs const&)
    {
        LoadHistory();
    }

    void HistoryPage::RefreshButton_Click(IInspectable const&, RoutedEventArgs const&)
    {
        LoadHistory();
    }

    fire_and_forget HistoryPage::ClearAllButton_Click(IInspectable const&, RoutedEventArgs const&)
    {
        auto strongThis = get_strong();

        ContentDialog dialog;
        dialog.Title(box_value(L"Clear all history?"));
        dialog.Content(box_value(L"This will remove all upload history entries. The files in S3 are not deleted."));
        dialog.PrimaryButtonText(L"Clear");
        dialog.CloseButtonText(L"Cancel");
        dialog.DefaultButton(ContentDialogButton::Close);
        dialog.XamlRoot(XamlRoot());

        auto result = co_await dialog.ShowAsync();
        if (result == ContentDialogResult::Primary)
        {
            HistoryManager().ClearAll();
            strongThis->LoadHistory();
        }
    }

    void HistoryPage::CopyLinkButton_Click(IInspectable const&, RoutedEventArgs const&)
    {
        // Unused — entries are built programmatically and wire their own click handlers.
    }

    fire_and_forget HistoryPage::ShareFileButton_Click(IInspectable const&, RoutedEventArgs const&)
    {
        auto strongThis = get_strong();

        Windows::Storage::Pickers::FileOpenPicker picker;
        picker.SuggestedStartLocation(Windows::Storage::Pickers::PickerLocationId::DocumentsLibrary);
        picker.FileTypeFilter().Append(L"*");

        // WinUI 3 desktop FilePickers need to be bound to a native window.
        auto initialize = picker.as<::IInitializeWithWindow>();
        initialize->Initialize(App::MainWindowHwnd());

        auto file = co_await picker.PickSingleFileAsync();
        if (!file)
        {
            co_return;
        }

        std::filesystem::path path{ std::wstring(file.Path()) };

        // Move the blocking S3 upload off the UI thread so the window doesn't freeze,
        // then resume on the original (UI) apartment to refresh the list.
        apartment_context ui_thread;
        co_await winrt::resume_background();
        ::Fling::Core::UploadCoordinator::UploadAndShare(path);
        co_await ui_thread;

        strongThis->LoadHistory();
    }

    void HistoryPage::LoadHistory()
    {
        HistoryManager hm;
        auto entries = hm.LoadEntries();

        auto items = HistoryListView().Items();
        items.Clear();

        for (auto const& entry : entries)
        {
            items.Append(BuildEntryItem(entry));
        }

        UpdateEmptyState(entries.empty());
    }

    void HistoryPage::UpdateEmptyState(bool empty)
    {
        EmptyStateText().Visibility(empty ? Visibility::Visible : Visibility::Collapsed);
        HistoryListView().Visibility(empty ? Visibility::Collapsed : Visibility::Visible);
    }

    IInspectable HistoryPage::BuildEntryItem(::Fling::Models::UploadEntry const& entry)
    {
        Grid row;
        row.Padding(ThicknessHelper::FromUniformLength(8));
        row.ColumnSpacing(12);

        ColumnDefinition nameCol; nameCol.Width(GridLengthHelper::FromValueAndType(1.0, GridUnitType::Star));
        ColumnDefinition statusCol; statusCol.Width(GridLengthHelper::Auto());
        ColumnDefinition buttonCol; buttonCol.Width(GridLengthHelper::Auto());
        row.ColumnDefinitions().Append(nameCol);
        row.ColumnDefinitions().Append(statusCol);
        row.ColumnDefinitions().Append(buttonCol);

        StackPanel info;
        info.VerticalAlignment(VerticalAlignment::Center);

        TextBlock name;
        name.Text(hstring(entry.fileName.empty() ? L"(unnamed)" : entry.fileName));
        name.FontWeight(Windows::UI::Text::FontWeights::SemiBold());
        info.Children().Append(name);

        TextBlock date;
        date.Text(hstring(FormatDate(entry.uploadedAt) + L" · " + FormatSize(entry.fileSize)));
        date.Opacity(0.7);
        info.Children().Append(date);

        if (!entry.success && !entry.errorMessage.empty())
        {
            TextBlock err;
            err.Text(hstring(entry.errorMessage));
            err.Opacity(0.7);
            info.Children().Append(err);
        }

        Grid::SetColumn(info, 0);
        row.Children().Append(info);

        TextBlock status;
        status.Text(hstring(ExpiryStatus(entry)));
        status.VerticalAlignment(VerticalAlignment::Center);
        if (!entry.success || entry.IsExpired())
        {
            status.Foreground(SolidColorBrush(Windows::UI::Colors::IndianRed()));
        }
        Grid::SetColumn(status, 1);
        row.Children().Append(status);

        Button copy;
        copy.Content(box_value(L"Copy Link"));
        copy.VerticalAlignment(VerticalAlignment::Center);
        copy.IsEnabled(entry.success && !entry.presignedUrl.empty() && !entry.IsExpired());

        std::wstring url = entry.presignedUrl;
        std::wstring fileName = entry.fileName;
        copy.Click([url, fileName](IInspectable const&, RoutedEventArgs const&)
        {
            if (ClipboardHelper::CopyToClipboard(url))
            {
                ToastHelper::ShowInfo(L"Link copied to clipboard");
            }
        });

        Grid::SetColumn(copy, 2);
        row.Children().Append(copy);

        ListViewItem item;
        item.Content(row);
        item.HorizontalContentAlignment(HorizontalAlignment::Stretch);
        return item;
    }
}
