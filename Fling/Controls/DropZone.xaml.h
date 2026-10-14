#pragma once
#include "DropZone.g.h"

#include <filesystem>
#include <functional>
#include <vector>

namespace winrt::Fling::implementation
{
    // Drop target that also opens the file chooser (Design/Components/DropZone.md).
    struct DropZone : DropZoneT<DropZone>
    {
        DropZone();

        std::function<void(std::vector<std::filesystem::path>)> FilesChosen;

        void Zone_DragOver(winrt::Windows::Foundation::IInspectable const&, winrt::Microsoft::UI::Xaml::DragEventArgs const&);
        void Zone_DragLeave(winrt::Windows::Foundation::IInspectable const&, winrt::Microsoft::UI::Xaml::DragEventArgs const&);
        winrt::fire_and_forget Zone_Drop(winrt::Windows::Foundation::IInspectable const&, winrt::Microsoft::UI::Xaml::DragEventArgs const&);
        void Zone_Tapped(winrt::Windows::Foundation::IInspectable const&, winrt::Microsoft::UI::Xaml::Input::TappedRoutedEventArgs const&);
        void Zone_KeyDown(winrt::Windows::Foundation::IInspectable const&, winrt::Microsoft::UI::Xaml::Input::KeyRoutedEventArgs const&);

    private:
        winrt::fire_and_forget PickFiles();
        void Deliver(std::vector<std::filesystem::path> files);
    };
}

namespace winrt::Fling::factory_implementation
{
    struct DropZone : DropZoneT<DropZone, implementation::DropZone>
    {
    };
}
