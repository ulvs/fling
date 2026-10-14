#include "pch.h"
#include "UploadProgressDialog.xaml.h"

#if __has_include("UploadProgressDialog.g.cpp")
#include "UploadProgressDialog.g.cpp"
#endif

using namespace winrt;
using namespace winrt::Microsoft::UI::Xaml;

namespace winrt::Fling::implementation
{
    UploadProgressDialog::UploadProgressDialog()
    {
        InitializeComponent();
    }

    void UploadProgressDialog::SetFileName(hstring const& /*fileName*/)
    {
        // Stub
    }

    void UploadProgressDialog::SetProgress(double /*progress*/)
    {
        // Stub
    }

    void UploadProgressDialog::SetStatus(hstring const& /*status*/)
    {
        // Stub
    }
}
