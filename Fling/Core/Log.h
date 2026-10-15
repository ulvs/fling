#pragma once
#include <filesystem>
#include <string>

namespace Fling::Core
{
    // Small local log for troubleshooting. Nothing is sent anywhere; users attach it to bug reports.
    // Never pass secrets, links or file names in messages: describe files by extension and size.
    class Log
    {
    public:
        static void Info(std::wstring const& message);
        static void Warning(std::wstring const& message);
        static void Error(std::wstring const& message);

        static std::filesystem::path Folder();

        // App version, Windows build, a settings summary without secrets, and the last log lines.
        static std::wstring DiagnosticInfo(size_t lastLines = 50);

        // Strips URL query strings (presigned signatures) and the user profile path.
        static std::wstring Scrub(std::wstring const& text);

        // "a .pdf file" / "a .pdf file (2.4 MB)"
        static std::wstring DescribeType(std::filesystem::path const& file);
        static std::wstring Describe(std::filesystem::path const& file, uint64_t size);
    };
}
