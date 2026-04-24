#pragma once
#include "UploadProgressDialog.g.h"

namespace winrt::Fling::implementation
{
    struct UploadProgressDialog : UploadProgressDialogT<UploadProgressDialog>
    {
        UploadProgressDialog();

        void SetFileName(hstring const& fileName);
        void SetProgress(double progress);
        void SetStatus(hstring const& status);
    };
}

namespace winrt::Fling::factory_implementation
{
    struct UploadProgressDialog : UploadProgressDialogT<UploadProgressDialog, implementation::UploadProgressDialog>
    {
    };
}
