#include "pch.h"
#include "HistoryManager.h"
#include <fstream>
#include <ShlObj.h>
#include <algorithm>
#include <nlohmann/json.hpp>

namespace Fling::Core
{
    HistoryManager::HistoryManager()
        : m_historyPath(GetDefaultHistoryPath())
    {
    }

    HistoryManager::HistoryManager(std::filesystem::path const& historyPath)
        : m_historyPath(historyPath)
    {
    }

    std::filesystem::path HistoryManager::GetDefaultHistoryPath()
    {
        // FLING_DATA_DIR points Fling at another data folder (tests, demo screenshots).
        wchar_t dataDir[MAX_PATH]{};
        if (GetEnvironmentVariableW(L"FLING_DATA_DIR", dataDir, MAX_PATH) > 0)
        {
            return std::filesystem::path(dataDir) / L"history.json";
        }

        wchar_t* localAppData = nullptr;
        if (SUCCEEDED(SHGetKnownFolderPath(FOLDERID_LocalAppData, 0, nullptr, &localAppData)))
        {
            std::filesystem::path path(localAppData);
            CoTaskMemFree(localAppData);
            return path / L"Fling" / L"history.json";
        }

        // Fallback to current directory
        return std::filesystem::current_path() / L"history.json";
    }

    void HistoryManager::EnsureDirectoryExists()
    {
        auto dir = m_historyPath.parent_path();
        if (!dir.empty() && !std::filesystem::exists(dir))
        {
            std::filesystem::create_directories(dir);
        }
    }

    std::vector<Models::UploadEntry> HistoryManager::LoadEntries()
    {
        if (!std::filesystem::exists(m_historyPath))
        {
            return {};
        }

        try
        {
            std::ifstream file(m_historyPath);
            nlohmann::json j;
            file >> j;

            std::vector<Models::UploadEntry> entries;
            if (j.is_array())
            {
                entries.reserve(j.size());
                for (auto const& item : j)
                {
                    entries.push_back(Models::UploadEntry::FromJson(item));
                }
            }
            return entries;
        }
        catch (...)
        {
            return {};
        }
    }

    void HistoryManager::SaveEntries(std::vector<Models::UploadEntry> const& entries)
    {
        EnsureDirectoryExists();

        try
        {
            nlohmann::json j = nlohmann::json::array();
            for (auto const& entry : entries)
            {
                j.push_back(entry.ToJson());
            }

            std::ofstream file(m_historyPath);
            if (file.is_open())
            {
                file << j.dump(2);
            }
        }
        catch (...)
        {
            // History write failures are non-fatal
        }
    }

    void HistoryManager::AddEntry(Models::UploadEntry const& entry)
    {
        auto entries = LoadEntries();

        // Add new entry at the beginning
        entries.insert(entries.begin(), entry);

        // Enforce maximum entries cap
        if (entries.size() > MAX_ENTRIES)
        {
            entries.resize(MAX_ENTRIES);
        }

        SaveEntries(entries);
    }

    void HistoryManager::RemoveExpired()
    {
        auto entries = LoadEntries();

        entries.erase(
            std::remove_if(entries.begin(), entries.end(),
                [](Models::UploadEntry const& e) { return e.IsExpired(); }),
            entries.end());

        SaveEntries(entries);
    }

    void HistoryManager::ClearAll()
    {
        SaveEntries({});
    }

    size_t HistoryManager::GetEntryCount()
    {
        return LoadEntries().size();
    }
}
