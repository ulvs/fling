#include "pch.h"
#include "FlingToast.xaml.h"

#if __has_include("FlingToast.g.cpp")
#include "FlingToast.g.cpp"
#endif

using namespace winrt;
using namespace winrt::Microsoft::UI::Xaml;
using namespace std::chrono_literals;

namespace winrt::Fling::implementation
{
    FlingToast::FlingToast()
    {
        InitializeComponent();
    }

    void FlingToast::Show(ToastTone tone, std::wstring const& message)
    {
        MessageText().Text(hstring(message));
        VisualStateManager::GoToState(*this, tone == ToastTone::Done ? L"DoneTone" : tone == ToastTone::Alert ? L"AlertTone" : L"Neutral", false);
        Visibility(Visibility::Visible);

        if (!m_timer)
        {
            m_timer = DispatcherQueue().CreateTimer();
            m_timer.IsRepeating(false);
            m_timer.Interval(4s);
            m_timer.Tick([weak = get_weak()](auto&&, auto&&)
            {
                if (auto self = weak.get())
                {
                    self->Visibility(Visibility::Collapsed);
                }
            });
        }
        m_timer.Stop();
        if (tone != ToastTone::Alert)
        {
            m_timer.Start();
        }
    }

    void FlingToast::Dismiss_Click(IInspectable const&, RoutedEventArgs const&)
    {
        if (m_timer)
        {
            m_timer.Stop();
        }
        Visibility(Visibility::Collapsed);
    }
}
