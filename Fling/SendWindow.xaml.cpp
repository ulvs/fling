#include "pch.h"
#include "SendWindow.xaml.h"

#if __has_include("SendWindow.g.cpp")
#include "SendWindow.g.cpp"
#endif

#include "App.xaml.h"
#include "Core/ClipboardHelper.h"
#include "Core/Format.h"
#include "Core/ToastHelper.h"
#include "Core/UploadCoordinator.h"

#include <atomic>
#include <memory>

using namespace winrt;
using namespace winrt::Microsoft::UI::Windowing;
using namespace winrt::Microsoft::UI::Xaml;
using namespace winrt::Microsoft::UI::Xaml::Controls;
using namespace std::chrono_literals;

namespace
{
    constexpr int kWidthDip = 420;
    constexpr int kHeightDip = 220;
    constexpr int kMarginDip = 16;

    constexpr wchar_t kGlyphDone[] = L"\uE73E";
    constexpr wchar_t kGlyphError[] = L"\uE783";
}

namespace winrt::Fling::implementation
{
    SendWindow::SendWindow()
    {
        InitializeComponent();

        auto appWindow = AppWindow();
        appWindow.SetIcon(L"Assets\\Fling.ico");
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
            if (auto self = weak.get(); self && self->m_uploading)
            {
                args.Cancel(true);
                self->m_dismissed = true;
                sender.Hide();
            }
        });

        PlaceInCorner();
    }

    void SendWindow::PlaceInCorner()
    {
        auto appWindow = AppWindow();
        HWND hwnd = winrt::Microsoft::UI::GetWindowFromWindowId(appWindow.Id());
        double scale = GetDpiForWindow(hwnd) / 96.0;

        POINT cursor{};
        GetCursorPos(&cursor);
        auto area = DisplayArea::GetFromPoint({ cursor.x, cursor.y }, DisplayAreaFallback::Primary).WorkArea();

        int width = static_cast<int>(kWidthDip * scale);
        int height = static_cast<int>(kHeightDip * scale);
        int margin = static_cast<int>(kMarginDip * scale);
        appWindow.MoveAndResize({ area.X + area.Width - width - margin, area.Y + area.Height - height - margin, width, height });
    }

    void SendWindow::Start(std::vector<std::filesystem::path> files)
    {
        Activate();
        RunUploads(std::move(files));
    }

    winrt::fire_and_forget SendWindow::RunUploads(std::vector<std::filesystem::path> files)
    {
        auto strongThis = get_strong();
        apartment_context uiThread;
        auto dispatcher = DispatcherQueue();
        m_uploading = true;

        std::vector<std::wstring> urls;
        std::vector<std::wstring> errors;
        size_t const total = files.size();

        TitleText().Text(total == 1 ? hstring(files[0].filename().wstring()) : hstring(std::to_wstring(total) + L" files"));

        for (size_t i = 0; i < total; ++i)
        {
            auto const& file = files[i];
            std::error_code ec;
            auto size = std::filesystem::file_size(file, ec);
            std::wstring detail = ec ? std::wstring() : ::Fling::Core::FormatSize(size);
            if (total > 1)
            {
                detail = file.filename().wstring() + L" (" + std::to_wstring(i + 1) + L" of " + std::to_wstring(total) + L")"
                    + (detail.empty() ? L"" : L" \u00B7 " + detail);
            }
            DetailText().Text(hstring(detail));
            StatusText().Text(L"Uploading\u2026");

            // Progress arrives on SDK threads; only marshal whole-percent changes to the UI.
            auto lastPercent = std::make_shared<std::atomic<int>>(-1);
            auto progress = [weak = get_weak(), dispatcher, lastPercent, i, total](uint64_t sent, uint64_t length)
            {
                if (length == 0)
                {
                    return;
                }
                double fileFraction = static_cast<double>(sent) / static_cast<double>(length);
                double overall = (static_cast<double>(i) + fileFraction) / static_cast<double>(total);
                int percent = static_cast<int>(overall * 100.0);
                if (lastPercent->exchange(percent) == percent)
                {
                    return;
                }
                dispatcher.TryEnqueue([weak, overall]
                {
                    if (auto self = weak.get())
                    {
                        self->SetProgress(overall);
                    }
                });
            };

            co_await winrt::resume_background();
            auto outcome = ::Fling::Core::UploadCoordinator::Upload(file, progress);
            co_await uiThread;

            if (outcome.success)
            {
                urls.push_back(outcome.url);
            }
            else
            {
                errors.push_back(total > 1 ? file.filename().wstring() + L": " + outcome.errorMessage : outcome.errorMessage);
            }
        }

        m_uploading = false;
        ShowResult(total, urls, errors);
    }

    void SendWindow::SetProgress(double fraction)
    {
        if (m_uploading)
        {
            UploadProgress().IsIndeterminate(false);
            UploadProgress().Value(fraction * 100.0);
            StatusText().Text(hstring(L"Uploading\u2026 " + std::to_wstring(static_cast<int>(fraction * 100.0)) + L"%"));
        }
    }

    void SendWindow::ShowResult(size_t total, std::vector<std::wstring> const& urls, std::vector<std::wstring> const& errors)
    {
        for (auto const& url : urls)
        {
            m_links += (m_links.empty() ? L"" : L"\r\n") + url;
        }
        bool copied = !m_links.empty() && ::Fling::Core::ClipboardHelper::CopyToClipboard(m_links);

        std::wstring summary;
        if (errors.empty())
        {
            summary = urls.size() == 1 ? L"Link copied to clipboard" : std::to_wstring(urls.size()) + L" links copied to clipboard";
            if (!copied)
            {
                summary = L"Uploaded, but the link could not be copied to the clipboard";
            }
        }
        else if (urls.empty())
        {
            summary = total == 1 ? L"Upload failed: " + errors[0] : L"All uploads failed. " + errors[0];
        }
        else
        {
            summary = std::to_wstring(urls.size()) + L" of " + std::to_wstring(total) + L" uploaded, links copied. " + errors[0];
        }

        if (m_dismissed)
        {
            ::Fling::Core::ToastHelper::ShowToast(L"Fling", summary);
            Close();
            return;
        }

        UploadProgress().IsIndeterminate(false);
        UploadProgress().Value(100.0);
        UploadProgress().ShowError(!errors.empty());
        StateIcon().Glyph(errors.empty() ? kGlyphDone : kGlyphError);
        auto brushKey = box_value(errors.empty() ? L"SystemFillColorSuccessBrush" : L"SystemFillColorCriticalBrush");
        StateIcon().Foreground(Application::Current().Resources().Lookup(brushKey).as<Media::Brush>());
        StatusText().Text(hstring(summary));

        if (!urls.empty())
        {
            LinkText().Text(hstring(urls.size() == 1 ? urls[0] : urls.back()));
            LinkText().Visibility(Visibility::Visible);
            CopyButton().Visibility(Visibility::Visible);
            CopyButton().Content(box_value(urls.size() == 1 ? L"Copy link" : L"Copy links"));
        }

        if (errors.empty())
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
        App::ShowMainWindow(L"history");
    }

    void SendWindow::SettingsButton_Click(IInspectable const&, RoutedEventArgs const&)
    {
        App::ShowMainWindow(L"settings");
    }

    void SendWindow::CopyButton_Click(IInspectable const&, RoutedEventArgs const&)
    {
        if (::Fling::Core::ClipboardHelper::CopyToClipboard(m_links))
        {
            StatusText().Text(L"Copied to clipboard");
        }
    }

    void SendWindow::CloseButton_Click(IInspectable const&, RoutedEventArgs const&)
    {
        // Window::Close() doesn't raise AppWindow.Closing, so apply the same keep-uploading rule here.
        if (m_uploading)
        {
            m_dismissed = true;
            AppWindow().Hide();
            return;
        }
        Close();
    }
}
