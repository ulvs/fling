#pragma once
#include <cstdint>
#include <ctime>
#include <filesystem>
#include <functional>
#include <string>
#include <vector>

namespace Fling::Core
{
    struct ArchiveResult
    {
        bool success{ false };
        std::filesystem::path path;
        std::wstring errorMessage;            // for the user; may contain a file name
        std::filesystem::path failedFile;      // the entry that couldn't be added, if any
    };

    class ArchiveBuilder
    {
    public:
        // "<folder>.zip" for a single folder, otherwise "flung-YYYY-MM-DD-HHMM.zip" (local time `when`).
        static std::wstring ArchiveName(std::vector<std::filesystem::path> const& items, std::time_t when = std::time(nullptr));

        // Total bytes of all regular files under the items (folders walked recursively).
        static uint64_t TotalSize(std::vector<std::filesystem::path> const& items);

        // Zips files and folders (recursively, each folder under its own name) into a fresh temp
        // folder as `archiveName`. Already-compressed formats are stored, everything else deflated.
        static ArchiveResult Build(std::vector<std::filesystem::path> const& items,
                                   std::wstring const& archiveName,
                                   std::function<void(uint64_t done, uint64_t total)> progress = nullptr);

        // Removes the temp folder Build created for `archivePath`.
        static void Cleanup(std::filesystem::path const& archivePath);
    };
}
