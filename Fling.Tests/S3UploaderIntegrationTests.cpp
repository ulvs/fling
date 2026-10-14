#include "pch.h"
#include "TestHelpers.h"
#include "Models/FlingConfig.h"
#include "Core/S3Uploader.h"

#include <winsock2.h>
#include <ws2tcpip.h>
#pragma comment(lib, "ws2_32.lib")

using namespace Fling::Core;
using namespace Fling::Models;

// Integration tests require MinIO running locally
// Run: docker compose up -d
// Create bucket: visit http://localhost:9001, login minioadmin/minioadmin, create "fling-test"

class S3UploaderIntegrationTests : public ::testing::Test
{
protected:
    std::filesystem::path m_testDir;
    FlingConfig m_config;

    void SetUp() override
    {
        m_testDir = TestHelpers::CreateTempDirectory();

        // MinIO test config
        TestHelpers::MinioConfig minio;
        m_config.endpoint = minio.endpoint;
        m_config.region = minio.region;
        m_config.bucket = minio.bucket;
        m_config.accessKey = minio.accessKey;
        m_config.secretKey = minio.secretKey;
        m_config.expirySeconds = 3600;
        m_config.usePathStyle = minio.usePathStyle;
    }

    void TearDown() override
    {
        TestHelpers::CleanupTempDirectory(m_testDir);
    }

    // Raw TCP probe: going through S3Uploader takes over a minute of SDK retries to fail.
    static bool IsMinioRunning()
    {
        static const bool running = []
        {
            WSADATA wsa{};
            if (WSAStartup(MAKEWORD(2, 2), &wsa) != 0)
            {
                return false;
            }
            bool ok = false;
            SOCKET s = socket(AF_INET, SOCK_STREAM, IPPROTO_TCP);
            if (s != INVALID_SOCKET)
            {
                u_long nonBlocking = 1;
                ioctlsocket(s, FIONBIO, &nonBlocking);
                sockaddr_in addr{};
                addr.sin_family = AF_INET;
                addr.sin_port = htons(9000);
                inet_pton(AF_INET, "127.0.0.1", &addr.sin_addr);
                connect(s, reinterpret_cast<sockaddr*>(&addr), sizeof(addr));
                fd_set writable;
                FD_ZERO(&writable);
                FD_SET(s, &writable);
                timeval timeout{ 0, 500'000 };
                ok = select(0, nullptr, &writable, nullptr, &timeout) == 1;
                closesocket(s);
            }
            WSACleanup();
            return ok;
        }();
        return running;
    }
};

TEST_F(S3UploaderIntegrationTests, TestConnection_Success)
{
    if (!IsMinioRunning())
    {
        GTEST_SKIP() << "MinIO not running, skipping integration test";
    }

    S3Uploader uploader(m_config);
    auto result = uploader.TestConnection();

    EXPECT_TRUE(result.success);
}

TEST_F(S3UploaderIntegrationTests, TestConnection_InvalidCredentials)
{
    if (!IsMinioRunning())
    {
        GTEST_SKIP() << "MinIO not running, skipping integration test";
    }

    auto badConfig = m_config;
    badConfig.accessKey = L"invalid";
    badConfig.secretKey = L"invalid";

    S3Uploader uploader(badConfig);
    auto result = uploader.TestConnection();

    EXPECT_FALSE(result.success);
}

TEST_F(S3UploaderIntegrationTests, TestConnection_InvalidBucket)
{
    if (!IsMinioRunning())
    {
        GTEST_SKIP() << "MinIO not running, skipping integration test";
    }

    auto badConfig = m_config;
    badConfig.bucket = L"nonexistent-bucket-12345";

    S3Uploader uploader(badConfig);
    auto result = uploader.TestConnection();

    EXPECT_FALSE(result.success);
}

TEST_F(S3UploaderIntegrationTests, Upload_SmallFile)
{
    if (!IsMinioRunning())
    {
        GTEST_SKIP() << "MinIO not running, skipping integration test";
    }

    auto testFile = TestHelpers::CreateTestFile(m_testDir, L"small.txt", 1024);

    S3Uploader uploader(m_config);
    auto result = uploader.Upload(testFile);

    EXPECT_TRUE(result.success);
    EXPECT_FALSE(result.presignedUrl.empty());
    EXPECT_FALSE(result.s3Key.empty());
}

TEST_F(S3UploaderIntegrationTests, Upload_WithProgressCallback)
{
    if (!IsMinioRunning())
    {
        GTEST_SKIP() << "MinIO not running, skipping integration test";
    }

    auto testFile = TestHelpers::CreateTestFile(m_testDir, L"progress.txt", 10 * 1024);

    bool progressCalled = false;
    uint64_t lastBytes = 0;

    S3Uploader uploader(m_config);
    auto result = uploader.Upload(testFile, [&](uint64_t transferred, uint64_t total)
    {
        progressCalled = true;
        EXPECT_GE(transferred, lastBytes);
        EXPECT_EQ(total, 10 * 1024);
        lastBytes = transferred;
    });

    EXPECT_TRUE(result.success);
    EXPECT_TRUE(progressCalled);
}

TEST_F(S3UploaderIntegrationTests, TestConnection_UnreachableEndpoint_FailsFast)
{
    auto badConfig = m_config;
    badConfig.endpoint = L"http://127.0.0.1:1";

    auto start = std::chrono::steady_clock::now();
    S3Uploader uploader(badConfig);
    auto result = uploader.TestConnection();
    auto elapsed = std::chrono::steady_clock::now() - start;

    EXPECT_FALSE(result.success);
    EXPECT_LT(elapsed, std::chrono::seconds(10));
}

TEST_F(S3UploaderIntegrationTests, Upload_NonexistentFile)
{
    S3Uploader uploader(m_config);
    auto result = uploader.Upload(m_testDir / L"nonexistent.txt");

    EXPECT_FALSE(result.success);
    EXPECT_FALSE(result.errorMessage.empty());
}

TEST_F(S3UploaderIntegrationTests, GeneratePresignedUrl)
{
    if (!IsMinioRunning())
    {
        GTEST_SKIP() << "MinIO not running, skipping integration test";
    }

    // First upload a file
    auto testFile = TestHelpers::CreateTestFile(m_testDir, L"presign.txt", 100);

    S3Uploader uploader(m_config);
    auto uploadResult = uploader.Upload(testFile);

    ASSERT_TRUE(uploadResult.success);

    // Generate new presigned URL
    auto url = uploader.GeneratePresignedUrl(uploadResult.s3Key);

    EXPECT_FALSE(url.empty());
    EXPECT_NE(url.find(L"http"), std::wstring::npos);
}

TEST_F(S3UploaderIntegrationTests, PresignedUrl_ContainsExpiration)
{
    if (!IsMinioRunning())
    {
        GTEST_SKIP() << "MinIO not running, skipping integration test";
    }

    auto testFile = TestHelpers::CreateTestFile(m_testDir, L"expiry.txt", 100);

    S3Uploader uploader(m_config);
    auto result = uploader.Upload(testFile);

    EXPECT_TRUE(result.success);
    // Presigned URL should contain expiration parameters
    EXPECT_NE(result.presignedUrl.find(L"X-Amz-Expires"), std::wstring::npos);
}

// Large file test - only run manually due to size
TEST_F(S3UploaderIntegrationTests, DISABLED_Upload_LargeFile)
{
    if (!IsMinioRunning())
    {
        GTEST_SKIP() << "MinIO not running, skipping integration test";
    }

    // Create 150MB file
    auto testFile = TestHelpers::CreateTestFile(m_testDir, L"large.bin", 150 * 1024 * 1024);

    S3Uploader uploader(m_config);
    auto result = uploader.Upload(testFile, [](uint64_t, uint64_t)
    {
    });

    EXPECT_TRUE(result.success);
    EXPECT_FALSE(result.presignedUrl.empty());
}
