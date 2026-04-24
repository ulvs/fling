#include "pch.h"
#include "ConfigManager.h"
#include <fstream>
#include <ShlObj.h>

namespace Fling::Core
{
    ConfigManager::ConfigManager()
        : m_configPath(GetDefaultConfigPath())
    {
    }

    ConfigManager::ConfigManager(std::filesystem::path const& configPath)
        : m_configPath(configPath)
    {
    }

    std::filesystem::path ConfigManager::GetDefaultConfigPath()
    {
        wchar_t* localAppData = nullptr;
        if (SUCCEEDED(SHGetKnownFolderPath(FOLDERID_LocalAppData, 0, nullptr, &localAppData)))
        {
            std::filesystem::path path(localAppData);
            CoTaskMemFree(localAppData);
            return path / L"Fling" / L"config.json";
        }

        // Fallback to current directory
        return std::filesystem::current_path() / L"config.json";
    }

    void ConfigManager::EnsureDirectoryExists()
    {
        auto dir = m_configPath.parent_path();
        if (!dir.empty() && !std::filesystem::exists(dir))
        {
            std::filesystem::create_directories(dir);
        }
    }

    Models::FlingConfig ConfigManager::Load()
    {
        if (m_cachedConfig.has_value())
        {
            return m_cachedConfig.value();
        }

        if (!std::filesystem::exists(m_configPath))
        {
            auto defaultConfig = Models::FlingConfig::Default();
            m_cachedConfig = defaultConfig;
            return defaultConfig;
        }

        try
        {
            std::ifstream file(m_configPath);
            nlohmann::json j;
            file >> j;
            auto config = Models::FlingConfig::FromJson(j);
            m_cachedConfig = config;
            return config;
        }
        catch (...)
        {
            auto defaultConfig = Models::FlingConfig::Default();
            m_cachedConfig = defaultConfig;
            return defaultConfig;
        }
    }

    bool ConfigManager::Save(Models::FlingConfig const& config)
    {
        EnsureDirectoryExists();

        try
        {
            std::ofstream file(m_configPath);
            if (!file.is_open())
            {
                return false;
            }
            file << config.ToJson().dump(2);
            if (!file.good())
            {
                return false;
            }
            m_cachedConfig = config;
            return true;
        }
        catch (...)
        {
            return false;
        }
    }

    bool ConfigManager::IsConfigured()
    {
        auto config = Load();
        return config.IsConfigured();
    }
}
