#include "pch.h"
#include "DropZone.xaml.h"

#if __has_include("DropZone.g.cpp")
#include "DropZone.g.cpp"
#endif

#include "App.xaml.h"

#include <shobjidl_core.h>
#include <winrt/Windows.ApplicationModel.DataTransfer.DragDrop.h>
#include <winrt/Windows.Storage.Pickers.h>

using namespace winrt;
using namespace winrt::Microsoft::UI::Xaml;
using namespace winrt::Windows::ApplicationModel::DataTransfer;
using namespace winrt::Windows::Storage;

namespace winrt::Fling::implementation
{
    DropZone::DropZone()
    {
        InitializeComponent();
    }

    void DropZone::Zone_DragOver(IInspectable const&, DragEventArgs const& e)
    {
        if (!e.DataView().Contains(StandardDataFormats::StorageItems()))
        {
            return;
        }
        e.AcceptedOperation(DataPackageOperation::Copy);
        e.DragUIOverride().Caption(L"Fling");
        VisualStateManager::GoToState(*this, L"Over", true);
    }

    void DropZone::Zone_DragLeave(IInspectable const&, DragEventArgs const&)
    {
        VisualStateManager::GoToState(*this, L"Idle", true);
    }

    fire_and_forget DropZone::Zone_Drop(IInspectable const&, DragEventArgs const& e)
    {
        auto strongThis = get_strong();
        VisualStateManager::GoToState(*this, L"Idle", true);
        if (!e.DataView().Contains(StandardDataFormats::StorageItems()))
        {
            co_return;
        }

        auto deferral = e.GetDeferral();
        auto items = co_await e.DataView().GetStorageItemsAsync();
        deferral.Complete();

        std::vector<std::filesystem::path> files;
        for (auto const& item : items)
        {
            if (item.IsOfType(StorageItemTypes::File) || item.IsOfType(StorageItemTypes::Folder))
            {
                files.emplace_back(std::wstring(item.Path()));
            }
        }
        strongThis->Deliver(std::move(files));
    }

    void DropZone::Zone_Tapped(IInspectable const&, Input::TappedRoutedEventArgs const&)
    {
        PickFiles();
    }

    void DropZone::Zone_KeyDown(IInspectable const&, Input::KeyRoutedEventArgs const& e)
    {
        if (e.Key() == Windows::System::VirtualKey::Enter || e.Key() == Windows::System::VirtualKey::Space)
        {
            e.Handled(true);
            PickFiles();
        }
    }

    fire_and_forget DropZone::PickFiles()
    {
        auto strongThis = get_strong();

        Pickers::FileOpenPicker picker;
        picker.FileTypeFilter().Append(L"*");
        // Desktop pickers must be bound to a window.
        picker.as<::IInitializeWithWindow>()->Initialize(App::MainWindowHwnd());

        auto picked = co_await picker.PickMultipleFilesAsync();
        std::vector<std::filesystem::path> files;
        for (auto const& file : picked)
        {
            files.emplace_back(std::wstring(file.Path()));
        }
        strongThis->Deliver(std::move(files));
    }

    void DropZone::Deliver(std::vector<std::filesystem::path> files)
    {
        if (!files.empty() && FilesChosen)
        {
            FilesChosen(std::move(files));
        }
    }
}
