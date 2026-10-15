#include "pch.h"
#include "SendWindow.xaml.h"

#if __has_include("SendWindow.g.cpp")
#include "SendWindow.g.cpp"
#endif

#include "App.xaml.h"
#include "Controls/TransferRow.xaml.h"
#include "Core/ArchiveBuilder.h"
#include "Core/ClipboardHelper.h"
#include "Core/ConfigManager.h"
#include "Core/Format.h"
#include "Core/ToastHelper.h"
#include "Core/UploadCoordinator.h"
#include "Pages/HistoryPage.xaml.h"

#include <atomic>
#include <chrono>
#include <ctime>
#include <iomanip>
#include <memory>
#include <sstream>

using namespace winrt;
using namespace winrt::Microsoft::UI::Windowing;
using namespace winrt::Microsoft::UI::Xaml;
using namespace std::chrono_literals;

namespace
{
    constexpr int kWidthDip = 440;
    constexpr int kChromeHeightDip = 150;  // title bar, header, footer and padding
    constexpr int kRowHeightDip = 88;
    constexpr size_t kMaxVisibleRows = 3;
    constexpr int kMarginDip = 16;
    constexpr wchar_t kSep[] = L" \u00B7 ";

    std::wstring FormatDuration(double seconds)
    {
        auto total = static_cast<long long>(seconds + 0.5);
        std::wostringstream os;
        if (total >= 3600)
        {
            os << total / 3600 << L':' << std::setw(2) << std::setfill(L'0') << (total % 3600) / 60 << L':';
        }
        else
        {
            os << total / 60 << L':';
        }
        os << std::setw(2) << std::setfill(L'0') << total % 60;
        return os.str();
    }

    std::wstring TimeOfDay()
    {
        std::time_t now = std::time(nullptr);
        std::tm tm{};
        localtime_s(&tm, &now);
        std::wostringstream os;
        os << std::put_time(&tm, L"%H:%M");
        return os.str();
    }

    std::wstring Count(size_t n, wchar_t const* one, wchar_t const* many)
    {
        return std::to_wstring(n) + L" " + (n == 1 ? one : many);
    }
}

namespace winrt::Fling::implementation
{
    SendWindow::SendWindow()
    {
        InitializeComponent();

        auto appWindow = AppWindow();
        appWindow.SetIcon(L"Assets\\Fling.ico");
        appWindow.TitleBar().PreferredTheme(TitleBarTheme::UseDefaultAppMode);
        if (auto presenter = appWindow.Presenter().try_as<OverlappedPresenter>())
        {
            presenter.IsResizable(false);
            presenter.IsMaximizable(false);
            presenter.IsMinimizable(false);
            presenter.IsAlwaysOnTop(true);
        }

        // Closing mid-upload would end the process (it may be the only window), so hide
        // instead and report the result as a toast when the upload finishes.
        appWindow.Closing([weak = get_weak()](auto const& sender, AppWindowClosingEventArgs const& args)
        {
            if (auto self = weak.get(); self && self->m_active > 0)
            {
                args.Cancel(true);
                self->m_dismissed = true;
                sender.Hide();
            }
        });
    }

    void SendWindow::PlaceInCorner(size_t rowCount)
    {
        auto appWindow = AppWindow();
        HWND hwnd = winrt::Microsoft::UI::GetWindowFromWindowId(appWindow.Id());
        double scale = GetDpiForWindow(hwnd) / 96.0;

        POINT cursor{};
        GetCursorPos(&cursor);
        auto area = DisplayArea::GetFromPoint({ cursor.x, cursor.y }, DisplayAreaFallback::Primary).WorkArea();

        int heightDip = kChromeHeightDip + static_cast<int>(std::min(rowCount, kMaxVisibleRows)) * kRowHeightDip;
        int width = static_cast<int>(kWidthDip * scale);
        int height = static_cast<int>(heightDip * scale);
        int margin = static_cast<int>(kMarginDip * scale);
        appWindow.MoveAndResize({ area.X + area.Width - width - margin, area.Y + area.Height - height - margin, width, height });
    }

    void SendWindow::Start(std::vector<std::filesystem::path> items)
    {
        using ::Fling::Core::ArchiveBuilder;
        auto config = ::Fling::Core::ConfigManager().Load();
        std::error_code ec;

        // A lone file goes as-is and folders are always zipped. Several items become one zip
        // unless the user chose separate links in Settings.
        std::vector<Job> jobs;
        auto addFile = [&](std::filesystem::path const& file)
        {
            Job job;
            job.path = file;
            job.name = file.filename().wstring();
            job.size = std::filesystem::file_size(file, ec);
            jobs.push_back(std::move(job));
        };
        auto addArchive = [&](std::vector<std::filesystem::path> sources)
        {
            Job job;
            job.archive = true;
            job.name = ArchiveBuilder::ArchiveName(sources);
            job.size = ArchiveBuilder::TotalSize(sources);
            job.sources = std::move(sources);
            jobs.push_back(std::move(job));
        };

        if (items.size() == 1 && std::filesystem::is_regular_file(items[0], ec))
        {
            addFile(items[0]);
        }
        else if (config.zipMultiple || items.size() == 1)
        {
            addArchive(items);
        }
        else
        {
            for (auto const& item : items)
            {
                if (std::filesystem::is_directory(item, ec))
                {
                    addArchive({ item });
                }
                else
                {
                    addFile(item);
                }
            }
        }

        for (size_t i = 0; i < jobs.size(); ++i)
        {
            auto& job = jobs[i];
            job.row = winrt::Fling::TransferRow();
            auto row = get_self<TransferRow>(job.row);
            row->SetFile(job.name, config.bucket);
            row->ShowWaiting(::Fling::Core::FormatSize(job.size) + kSep + L"Waiting");
            row->ActionInvoked = [weak = get_weak(), i]
            {
                if (auto self = weak.get())
                {
                    self->Retry(i);
                }
            };
            Rows().Children().Append(job.row);
        }
        m_jobs = std::move(jobs);

        TitleText().Text(hstring(m_jobs.size() == 1
            ? L"Uploading " + m_jobs[0].name
            : L"Uploading " + Count(m_jobs.size(), L"file", L"files")));
        PlaceInCorner(m_jobs.size());
        Activate();
        RunAll();
    }

    winrt::Windows::Foundation::IAsyncAction SendWindow::RunJob(size_t index)
    {
        auto strongThis = get_strong();
        apartment_context uiThread;
        auto dispatcher = DispatcherQueue();

        ++m_active;
        if (m_autoCloseTimer)
        {
            m_autoCloseTimer.Stop();
        }

        std::filesystem::path path = m_jobs[index].path;
        bool archive = m_jobs[index].archive;
        if (archive)
        {
            get_self<TransferRow>(m_jobs[index].row)->ShowSending(0.0, std::wstring(L"Zipping") + kSep + L"0%");
            StatusText().Text(L"Zipping\u2026");

            auto zipPercent = std::make_shared<std::atomic<int>>(-1);
            auto zipProgress = [weak = get_weak(), dispatcher, zipPercent, index](uint64_t done, uint64_t total)
            {
                if (total == 0)
                {
                    return;
                }
                double fraction = static_cast<double>(done) / static_cast<double>(total);
                int percent = static_cast<int>(fraction * 100.0);
                if (zipPercent->exchange(percent) == percent)
                {
                    return;
                }
                std::wstring detail = std::wstring(L"Zipping") + kSep + std::to_wstring(percent) + L"%";
                dispatcher.TryEnqueue([weak, index, fraction, detail]
                {
                    if (auto self = weak.get())
                    {
                        get_self<TransferRow>(self->m_jobs[index].row)->ShowSending(fraction, detail);
                    }
                });
            };

            auto sources = m_jobs[index].sources;
            auto name = m_jobs[index].name;
            co_await resume_background();
            auto built = ::Fling::Core::ArchiveBuilder::Build(sources, name, zipProgress);
            co_await uiThread;

            if (!built.success)
            {
                --m_active;
                m_jobs[index].succeeded = false;
                m_jobs[index].error = built.errorMessage;
                get_self<TransferRow>(m_jobs[index].row)->ShowFailed(built.errorMessage);
                co_return;
            }
            path = built.path;
            std::error_code ec;
            m_jobs[index].size = std::filesystem::file_size(path, ec);
        }

        auto sizeText = ::Fling::Core::FormatSize(m_jobs[index].size);
        get_self<TransferRow>(m_jobs[index].row)->ShowSending(0.0, sizeText + kSep + L"0%");
        StatusText().Text(L"Uploading\u2026");

        // Progress arrives on SDK threads; only marshal whole-percent changes to the UI.
        auto lastPercent = std::make_shared<std::atomic<int>>(-1);
        auto started = std::chrono::steady_clock::now();
        auto progress = [weak = get_weak(), dispatcher, lastPercent, started, index, sizeText](uint64_t sent, uint64_t total)
        {
            if (total == 0)
            {
                return;
            }
            double fraction = static_cast<double>(sent) / static_cast<double>(total);
            int percent = static_cast<int>(fraction * 100.0);
            if (lastPercent->exchange(percent) == percent)
            {
                return;
            }

            std::wstring detail = sizeText + kSep + std::to_wstring(percent) + L"%";
            double elapsed = std::chrono::duration<double>(std::chrono::steady_clock::now() - started).count();
            if (elapsed > 0.5 && sent > 0)
            {
                double speed = sent / elapsed;
                detail += kSep + ::Fling::Core::FormatSize(static_cast<uint64_t>(speed)) + L"/s"
                    + kSep + FormatDuration((total - sent) / speed) + L" left";
            }

            dispatcher.TryEnqueue([weak, index, fraction, detail]
            {
                if (auto self = weak.get())
                {
                    get_self<TransferRow>(self->m_jobs[index].row)->ShowSending(fraction, detail);
                }
            });
        };

        co_await resume_background();
        auto outcome = ::Fling::Core::UploadCoordinator::Upload(path, progress);
        if (archive)
        {
            ::Fling::Core::ArchiveBuilder::Cleanup(path);
        }
        co_await uiThread;

        --m_active;
        auto& job = m_jobs[index];
        auto row = get_self<TransferRow>(job.row);
        job.succeeded = outcome.success;
        if (outcome.success)
        {
            job.url = outcome.url;
            job.error.clear();
            row->ShowDone(sizeText + kSep + L"Uploaded " + TimeOfDay());
        }
        else
        {
            job.error = outcome.errorMessage;
            row->ShowFailed(L"Couldn't upload. " + outcome.errorMessage);
        }
    }

    winrt::fire_and_forget SendWindow::RunAll()
    {
        auto strongThis = get_strong();
        for (size_t i = 0; i < m_jobs.size(); ++i)
        {
            co_await RunJob(i);
        }
        Finish();
    }

    winrt::fire_and_forget SendWindow::Retry(size_t index)
    {
        auto strongThis = get_strong();
        co_await RunJob(index);
        Finish();
    }

    void SendWindow::Finish()
    {
        if (m_active > 0)
        {
            return;
        }

        m_links.clear();
        size_t ok = 0;
        for (auto const& job : m_jobs)
        {
            if (job.succeeded)
            {
                m_links += (m_links.empty() ? L"" : L"\r\n") + job.url;
                ++ok;
            }
        }
        size_t total = m_jobs.size();
        size_t failed = total - ok;
        bool copied = ok > 0 && ::Fling::Core::ClipboardHelper::CopyToClipboard(m_links);

        std::wstring summary;
        if (ok > 0)
        {
            summary = copied ? (ok == 1 ? L"Link copied" : std::to_wstring(ok) + L" links copied")
                             : L"Uploaded, but the clipboard was busy. Use Copy link";
        }
        if (failed > 0)
        {
            summary += (summary.empty() ? L"" : kSep) + Count(failed, L"file", L"files") + L" didn't upload";
        }

        HistoryPage::RefreshVisible();

        if (m_dismissed)
        {
            ::Fling::Core::ToastHelper::ShowToast(L"Fling", summary);
            Close();
            return;
        }

        TitleText().Text(hstring(failed == 0
            ? L"Uploaded " + Count(total, L"file", L"files")
            : L"Couldn't upload " + Count(failed, L"file", L"files")));
        StatusText().Text(hstring(summary));
        CopyButton().Visibility(ok > 0 ? Visibility::Visible : Visibility::Collapsed);
        CopyButton().Content(box_value(ok == 1 ? L"Copy link" : L"Copy links"));

        if (failed == 0)
        {
            if (!m_autoCloseTimer)
            {
                m_autoCloseTimer = DispatcherQueue().CreateTimer();
                m_autoCloseTimer.Interval(6s);
                m_autoCloseTimer.IsRepeating(false);
                m_autoCloseTimer.Tick([weak = get_weak()](auto&&, auto&&)
                {
                    if (auto self = weak.get())
                    {
                        self->Close();
                    }
                });
            }
            m_autoCloseTimer.Start();
        }
    }

    void SendWindow::Root_PointerEntered(IInspectable const&, Input::PointerRoutedEventArgs const&)
    {
        if (m_autoCloseTimer)
        {
            m_autoCloseTimer.Stop();
        }
    }

    void SendWindow::InfoButton_Click(IInspectable const&, RoutedEventArgs const&)
    {
        App::ShowMainWindow(L"uploads");
    }

    void SendWindow::SettingsButton_Click(IInspectable const&, RoutedEventArgs const&)
    {
        App::ShowMainWindow(L"settings");
    }

    void SendWindow::CopyButton_Click(IInspectable const&, RoutedEventArgs const&)
    {
        if (::Fling::Core::ClipboardHelper::CopyToClipboard(m_links))
        {
            StatusText().Text(L"Copied to the clipboard");
        }
    }

    void SendWindow::CloseButton_Click(IInspectable const&, RoutedEventArgs const&)
    {
        // Window::Close() doesn't raise AppWindow.Closing, so apply the same keep-uploading rule here.
        if (m_active > 0)
        {
            m_dismissed = true;
            AppWindow().Hide();
            return;
        }
        Close();
    }
}
