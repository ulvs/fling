#pragma once
#include "../Models/FlingConfig.h"
#include <filesystem>
#include <optional>

namespace Fling::Core
{
    class ConfigManager
    {
    public:
        ConfigManager();
        explicit ConfigManager(std::filesystem::path const& configPath);

        // Load configuration from disk
        // Returns default config if file doesn't exist
        Models::FlingConfig Load();

        // Save configuration to disk
        bool Save(Models::FlingConfig const& config);

        // Check if configuration exists and is valid
        bool IsConfigured();

        // Get the configuration file path
        std::filesystem::path GetConfigPath() const { return m_configPath; }

    private:
        std::filesystem::path m_configPath;
        std::optional<Models::FlingConfig> m_cachedConfig;

        static std::filesystem::path GetDefaultConfigPath();
        void EnsureDirectoryExists();
    };
}
