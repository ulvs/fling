#pragma once
#include <filesystem>
#include <fstream>
#include <random>

namespace TestHelpers
{
    // Create a temporary directory for tests
    inline std::filesystem::path CreateTempDirectory()
    {
        auto tempPath = std::filesystem::temp_directory_path();
        std::random_device rd;
        std::mt19937 gen(rd());
        std::uniform_int_distribution<> dis(0, 999999);

        auto testDir = tempPath / (L"FlingTest_" + std::to_wstring(dis(gen)));
        std::filesystem::create_directories(testDir);
        return testDir;
    }

    // Create a test file with random content
    inline std::filesystem::path CreateTestFile(
        std::filesystem::path const& dir,
        std::wstring const& filename,
        size_t sizeBytes = 1024)
    {
        auto filePath = dir / filename;
        std::ofstream file(filePath, std::ios::binary);

        std::random_device rd;
        std::mt19937 gen(rd());
        std::uniform_int_distribution<> dis(0, 255);

        for (size_t i = 0; i < sizeBytes; ++i)
        {
            char c = static_cast<char>(dis(gen));
            file.write(&c, 1);
        }

        file.close();
        return filePath;
    }

    // Clean up test directory
    inline void CleanupTempDirectory(std::filesystem::path const& dir)
    {
        if (std::filesystem::exists(dir))
        {
            std::filesystem::remove_all(dir);
        }
    }

    // MinIO test configuration
    struct MinioConfig
    {
        std::wstring endpoint = L"http://localhost:9000";
        std::wstring region = L"us-east-1";
        std::wstring bucket = L"fling-test";
        std::wstring accessKey = L"minioadmin";
        std::wstring secretKey = L"minioadmin";
        bool usePathStyle = true;
    };
}
