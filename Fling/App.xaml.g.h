#pragma once
// This file intentionally overrides the XAML compiler's generated App.xaml.g.h.
//
// WindowsAppSDK 1.6's markup compiler emits a legacy UWP-style AppT for
// ApplicationDefinition that inherits directly from ApplicationT, bypassing
// App_base. That breaks make<implementation::App>() — it returns
// Microsoft::UI::Xaml::Application instead of Fling::App, so the generated
// App.g.cpp's delegating constructor fails to compile.
//
// This override re-defines AppT so it chains through App_base, matching the
// pattern the markup compiler uses for every other XAML class (MainWindow,
// SettingsPage, etc.). We keep the InitializeComponent + metadata-provider
// plumbing from the stock output so XAML type resolution still works.

#include <unknwn.h>

// Undefine GetCurrentTime macro to prevent conflict with Storyboard::GetCurrentTime
#undef GetCurrentTime

#include "winrt/Microsoft.UI.Xaml.h"
#include "winrt/Microsoft.UI.Xaml.Markup.h"
#include "winrt/Microsoft.UI.Xaml.Interop.h"
#include "winrt/Windows.UI.Xaml.Interop.h"

#include "XamlTypeInfo.xaml.g.h"
#include "XamlMetaDataProvider.h"

namespace winrt::Fling::implementation
{
    template <typename D, typename... Interfaces>
    struct AppT : public App_base<D, ::winrt::Microsoft::UI::Xaml::Markup::IXamlMetadataProvider, Interfaces...>
    {
        using base_type = typename AppT::base_type;
        using base_type::base_type;
        using class_type = typename AppT::class_type;

        using IXamlType = ::winrt::Microsoft::UI::Xaml::Markup::IXamlType;

        void InitializeComponent()
        {
            if (_contentLoaded)
                return;

            _contentLoaded = true;

            ::winrt::Windows::Foundation::Uri resourceLocator{ L"ms-appx:///App.xaml" };
            ::winrt::Microsoft::UI::Xaml::Application::LoadComponent(*this, resourceLocator);
        }

        IXamlType GetXamlType(::winrt::Windows::UI::Xaml::Interop::TypeName const& type)
        {
            return AppProvider()->GetXamlType(type);
        }

        IXamlType GetXamlType(::winrt::hstring const& fullName)
        {
            return AppProvider()->GetXamlType(fullName);
        }

        ::winrt::com_array<::winrt::Microsoft::UI::Xaml::Markup::XmlnsDefinition> GetXmlnsDefinitions()
        {
            return AppProvider()->GetXmlnsDefinitions();
        }

    private:
        bool _contentLoaded{ false };
        winrt::com_ptr<XamlMetaDataProvider> _appProvider;
        winrt::com_ptr<XamlMetaDataProvider> AppProvider()
        {
            if (!_appProvider)
            {
                _appProvider = winrt::make_self<XamlMetaDataProvider>();
            }
            return _appProvider;
        }
    };
}
