#pragma once
#include <string>

namespace Fling::Core
{
    class ClipboardHelper
    {
    public:
        // Copy text to clipboard
        static bool CopyToClipboard(std::wstring const& text);

        // Get text from clipboard
        static std::wstring GetFromClipboard();
    };
}
