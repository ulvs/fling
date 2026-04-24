#include "pch.h"
#include "ClipboardHelper.h"

#include <winrt/Windows.ApplicationModel.DataTransfer.h>

using namespace winrt::Windows::ApplicationModel::DataTransfer;

namespace Fling::Core
{
    bool ClipboardHelper::CopyToClipboard(std::wstring const& text)
    {
        try
        {
            DataPackage dataPackage;
            dataPackage.SetText(winrt::hstring(text));
            Clipboard::SetContent(dataPackage);
            Clipboard::Flush();
            return true;
        }
        catch (winrt::hresult_error const&)
        {
            return false;
        }
    }

    std::wstring ClipboardHelper::GetFromClipboard()
    {
        try
        {
            auto content = Clipboard::GetContent();
            if (content.Contains(StandardDataFormats::Text()))
            {
                auto text = content.GetTextAsync().get();
                return std::wstring(text);
            }
            return L"";
        }
        catch (winrt::hresult_error const&)
        {
            return L"";
        }
    }
}
