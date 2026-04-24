#include "pch.h"
#include "TestHelpers.h"
#include "Core/ConfigManager.h"

using namespace Fling::Core;
using namespace Fling::Models;

class ConfigManagerTests : public ::testing::Test
{
protected:
    std::filesystem::path m_testDir;

    void SetUp() override
    {
        m_testDir = TestHelpers::CreateTempDirectory();
    }

    void TearDown() override
    {
        TestHelpers::CleanupTempDirectory(m_testDir);
    }
};

TEST_F(ConfigManagerTests, Load_MissingFile_CreatesDefault)
{
    auto configPath = m_testDir / L"config.json";
    ConfigManager manager(configPath);

    auto config = manager.Load();

    // Should create default config
    EXPECT_TRUE(std::filesystem::exists(configPath));
    EXPECT_EQ(config.endpoint, L"https://s3.amazonaws.com");
    EXPECT_EQ(config.region, L"us-east-1");
}

TEST_F(ConfigManagerTests, SaveAndLoad_RoundTrip)
{
    auto configPath = m_testDir / L"config.json";
    ConfigManager manager(configPath);

    FlingConfig original;
    original.endpoint = L"http://localhost:9000";
    original.region = L"eu-central-1";
    original.bucket = L"test-bucket";
    original.accessKey = L"AKIAEXAMPLE";
    original.secretKey = L"secretkey123";
    original.expirySeconds = 86400;
    original.usePathStyle = true;

    EXPECT_TRUE(manager.Save(original));

    // Create new manager to ensure we read from disk
    ConfigManager manager2(configPath);
    auto loaded = manager2.Load();

    EXPECT_EQ(loaded.endpoint, original.endpoint);
    EXPECT_EQ(loaded.region, original.region);
    EXPECT_EQ(loaded.bucket, original.bucket);
    EXPECT_EQ(loaded.accessKey, original.accessKey);
    EXPECT_EQ(loaded.secretKey, original.secretKey);
    EXPECT_EQ(loaded.expirySeconds, original.expirySeconds);
    EXPECT_EQ(loaded.usePathStyle, original.usePathStyle);
}

TEST_F(ConfigManagerTests, IsConfigured_ValidConfig)
{
    auto configPath = m_testDir / L"config.json";
    ConfigManager manager(configPath);

    FlingConfig config;
    config.endpoint = L"http://localhost:9000";
    config.bucket = L"test-bucket";
    config.accessKey = L"AKIAEXAMPLE";
    config.secretKey = L"secretkey123";

    manager.Save(config);

    EXPECT_TRUE(manager.IsConfigured());
}

TEST_F(ConfigManagerTests, IsConfigured_DefaultConfig)
{
    auto configPath = m_testDir / L"config.json";
    ConfigManager manager(configPath);

    // Load creates default which is not configured
    manager.Load();

    EXPECT_FALSE(manager.IsConfigured());
}

TEST_F(ConfigManagerTests, Load_CorruptedJson_ReturnsDefault)
{
    auto configPath = m_testDir / L"config.json";

    // Write corrupted JSON
    std::ofstream file(configPath);
    file << "{ invalid json }}}";
    file.close();

    ConfigManager manager(configPath);
    auto config = manager.Load();

    // Should return default
    EXPECT_EQ(config.endpoint, L"https://s3.amazonaws.com");
}

TEST_F(ConfigManagerTests, Save_CreatesDirectories)
{
    auto configPath = m_testDir / L"subdir" / L"nested" / L"config.json";
    ConfigManager manager(configPath);

    FlingConfig config;
    config.endpoint = L"http://test.com";
    config.bucket = L"bucket";
    config.accessKey = L"key";
    config.secretKey = L"secret";

    EXPECT_TRUE(manager.Save(config));
    EXPECT_TRUE(std::filesystem::exists(configPath));
}

TEST_F(ConfigManagerTests, GetConfigPath)
{
    auto configPath = m_testDir / L"config.json";
    ConfigManager manager(configPath);

    EXPECT_EQ(manager.GetConfigPath(), configPath);
}

TEST_F(ConfigManagerTests, Load_CachesResult)
{
    auto configPath = m_testDir / L"config.json";
    ConfigManager manager(configPath);

    FlingConfig config;
    config.endpoint = L"http://test.com";
    config.bucket = L"bucket";
    config.accessKey = L"key";
    config.secretKey = L"secret";
    manager.Save(config);

    // Load once
    auto loaded1 = manager.Load();

    // Modify file directly
    std::ofstream file(configPath);
    file << R"({"endpoint":"http://modified.com","bucket":"b","accessKey":"a","secretKey":"s"})";
    file.close();

    // Load again - should return cached
    auto loaded2 = manager.Load();

    EXPECT_EQ(loaded1.endpoint, loaded2.endpoint);
    EXPECT_EQ(loaded2.endpoint, L"http://test.com");  // Cached value
}
