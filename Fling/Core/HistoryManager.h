#pragma once
#include "../Models/UploadEntry.h"
#include <filesystem>
#include <vector>

namespace Fling::Core
{
    class HistoryManager
    {
    public:
        HistoryManager();
        explicit HistoryManager(std::filesystem::path const& historyPath);

        // Load all history entries
        std::vector<Models::UploadEntry> LoadEntries();

        // Add a new entry (saves immediately)
        void AddEntry(Models::UploadEntry const& entry);

        // Remove expired entries
        void RemoveExpired();

        // Clear all history
        void ClearAll();

        // Get entry count
        size_t GetEntryCount();

        // Get the history file path
        std::filesystem::path GetHistoryPath() const { return m_historyPath; }

        static constexpr size_t MAX_ENTRIES = 500;

    private:
        std::filesystem::path m_historyPath;

        static std::filesystem::path GetDefaultHistoryPath();
        void EnsureDirectoryExists();
        void SaveEntries(std::vector<Models::UploadEntry> const& entries);
    };
}
