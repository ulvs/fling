#include "pch.h"
#include "HistoryPage.xaml.h"

#if __has_include("HistoryPage.g.cpp")
#include "HistoryPage.g.cpp"
#endif

#include "App.xaml.h"
#include "Controls/DropZone.xaml.h"
#include "Controls/FlingToast.xaml.h"
#include "Controls/TransferRow.xaml.h"
#include "Core/ClipboardHelper.h"
#include "Core/Format.h"
#include "Core/HistoryManager.h"

#include <chrono>
#include <ctime>
#include <iomanip>
#include <sstream>

using namespace winrt;
using namespace winrt::Microsoft::UI::Xaml;
using namespace winrt::Microsoft::UI::Xaml::Controls;
using namespace winrt::Microsoft::UI::Xaml::Navigation;
using namespace ::Fling::Core;

namespace
{
    constexpr size_t kMaxRows = 100;

    int64_t NowSeconds()
    {
        using namespace std::chrono;
        return duration_cast<seconds>(system_clock::now().time_since_epoch()).count();
    }

    // "14:02" for today, "2026-04-23" otherwise.
    std::wstring FormatWhen(int64_t epochSeconds)
    {
        if (epochSeconds <= 0)
        {
            return L"unknown date";
        }
        std::time_t t = static_cast<std::time_t>(epochSeconds);
        std::time_t now = static_cast<std::time_t>(NowSeconds());
        std::tm tm{};
        std::tm today{};
        localtime_s(&tm, &t);
        localtime_s(&today, &now);
        std::wostringstream os;
        bool sameDay = tm.tm_year == today.tm_year && tm.tm_yday == today.tm_yday;
        os << std::put_time(&tm, sameDay ? L"%H:%M" : L"%Y-%m-%d");
        return os.str();
    }

    std::wstring TimeLeft(int64_t expiresAt)
    {
        auto remaining = expiresAt - NowSeconds();
        if (remaining >= 86400) return std::to_wstring(remaining / 86400) + L"d left";
        if (remaining >= 3600) return std::to_wstring(remaining / 3600) + L"h left";
        return std::to_wstring(std::max<int64_t>(remaining / 60, 1)) + L"m left";
    }
}

namespace winrt::Fling::implementation
{
    winrt::weak_ref<HistoryPage> HistoryPage::s_visible;

    HistoryPage::HistoryPage()
    {
        InitializeComponent();
        get_self<DropZone>(Drop())->FilesChosen = [](std::vector<std::filesystem::path> files)
        {
            App::ShowSendWindow(std::move(files));
        };
    }

    void HistoryPage::OnNavigatedTo(NavigationEventArgs const&)
    {
        s_visible = get_weak();
        LoadHistory();
    }

    void HistoryPage::OnNavigatedFrom(NavigationEventArgs const&)
    {
        s_visible = nullptr;
    }

    void HistoryPage::RefreshVisible()
    {
        if (auto page = s_visible.get())
        {
            page->LoadHistory();
        }
    }

    fire_and_forget HistoryPage::ClearButton_Click(IInspectable const&, RoutedEventArgs const&)
    {
        auto strongThis = get_strong();

        ContentDialog dialog;
        dialog.Title(box_value(L"Clear the list?"));
        dialog.Content(box_value(L"This removes the list on this PC. Uploaded files stay in your storage, and shared links keep working until they expire."));
        dialog.PrimaryButtonText(L"Clear list");
        dialog.CloseButtonText(L"Cancel");
        dialog.DefaultButton(ContentDialogButton::Close);
        dialog.XamlRoot(XamlRoot());

        if (co_await dialog.ShowAsync() == ContentDialogResult::Primary)
        {
            HistoryManager().ClearAll();
            strongThis->LoadHistory();
        }
    }

    void HistoryPage::LoadHistory()
    {
        auto entries = HistoryManager().LoadEntries();

        auto rows = Rows().Children();
        rows.Clear();
        for (size_t i = 0; i < entries.size() && i < kMaxRows; ++i)
        {
            rows.Append(BuildRow(entries[i]));
        }

        EmptyText().Visibility(entries.empty() ? Visibility::Visible : Visibility::Collapsed);
        ClearButton().Visibility(entries.empty() ? Visibility::Collapsed : Visibility::Visible);
    }

    winrt::Fling::TransferRow HistoryPage::BuildRow(::Fling::Models::UploadEntry const& entry)
    {
        winrt::Fling::TransferRow row;
        auto self = get_self<TransferRow>(row);
        self->SetFile(entry.fileName.empty() ? L"(unnamed)" : entry.fileName, {});

        std::wstring size = FormatSize(entry.fileSize);
        std::wstring when = L"Uploaded " + FormatWhen(entry.uploadedAt);

        if (!entry.success)
        {
            std::filesystem::path path(entry.filePath);
            std::error_code ec;
            bool canRetry = !entry.filePath.empty() && std::filesystem::is_regular_file(path, ec);
            self->ShowFailed(L"Couldn't upload. " + entry.errorMessage, canRetry ? L"Try again" : L"");
            self->ActionInvoked = [path]
            {
                App::ShowSendWindow({ path });
            };
        }
        else if (entry.IsExpired())
        {
            self->ShowInactive(size + L" \u00B7 " + when + L" \u00B7 link expired");
        }
        else
        {
            self->ShowDone(size + L" \u00B7 " + when + L" \u00B7 " + TimeLeft(entry.expiresAt), L"Copy link");
            self->ActionInvoked = [weak = get_weak(), url = entry.presignedUrl]
            {
                auto page = weak.get();
                if (!page)
                {
                    return;
                }
                bool copied = ClipboardHelper::CopyToClipboard(url);
                get_self<FlingToast>(page->Toast())->Show(
                    copied ? ToastTone::Neutral : ToastTone::Alert,
                    copied ? L"Link copied" : L"Couldn't copy the link");
            };
        }
        return row;
    }
}
