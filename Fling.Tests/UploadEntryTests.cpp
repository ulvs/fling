#include "pch.h"
#include "Models/UploadEntry.h"

using namespace Fling::Models;

class UploadEntryTests : public ::testing::Test
{
protected:
    void SetUp() override {}
    void TearDown() override {}
};

TEST_F(UploadEntryTests, CreateSuccess)
{
    auto entry = UploadEntry::CreateSuccess(
        L"test.txt",
        L"C:\\path\\to\\test.txt",
        L"abc123/test.txt",
        L"https://example.com/presigned",
        1024,
        3600);

    EXPECT_EQ(entry.fileName, L"test.txt");
    EXPECT_EQ(entry.filePath, L"C:\\path\\to\\test.txt");
    EXPECT_EQ(entry.s3Key, L"abc123/test.txt");
    EXPECT_EQ(entry.presignedUrl, L"https://example.com/presigned");
    EXPECT_EQ(entry.fileSize, 1024);
    EXPECT_TRUE(entry.success);
    EXPECT_TRUE(entry.errorMessage.empty());
    EXPECT_GT(entry.uploadedAt, 0);
    EXPECT_GT(entry.expiresAt, entry.uploadedAt);
}

TEST_F(UploadEntryTests, CreateFailure)
{
    auto entry = UploadEntry::CreateFailure(
        L"test.txt",
        L"C:\\path\\to\\test.txt",
        L"Connection refused");

    EXPECT_EQ(entry.fileName, L"test.txt");
    EXPECT_EQ(entry.filePath, L"C:\\path\\to\\test.txt");
    EXPECT_FALSE(entry.success);
    EXPECT_EQ(entry.errorMessage, L"Connection refused");
    EXPECT_TRUE(entry.presignedUrl.empty());
}

TEST_F(UploadEntryTests, IsExpired_NotExpired)
{
    auto entry = UploadEntry::CreateSuccess(
        L"test.txt",
        L"C:\\test.txt",
        L"key",
        L"url",
        100,
        3600);  // Expires in 1 hour

    EXPECT_FALSE(entry.IsExpired());
}

TEST_F(UploadEntryTests, IsExpired_Expired)
{
    UploadEntry entry;
    entry.uploadedAt = 0;  // Epoch
    entry.expiresAt = 1;   // 1 second after epoch (long expired)

    EXPECT_TRUE(entry.IsExpired());
}

TEST_F(UploadEntryTests, JsonRoundTrip)
{
    UploadEntry original;
    original.fileName = L"document.pdf";
    original.filePath = L"D:\\Documents\\document.pdf";
    original.s3Key = L"uuid/document.pdf";
    original.presignedUrl = L"https://bucket.s3.amazonaws.com/uuid/document.pdf?signature=xxx";
    original.fileSize = 2048576;
    original.uploadedAt = 1700000000;
    original.expiresAt = 1700604800;
    original.success = true;

    nlohmann::json j = original.ToJson();
    UploadEntry restored = UploadEntry::FromJson(j);

    EXPECT_EQ(restored.fileName, original.fileName);
    EXPECT_EQ(restored.filePath, original.filePath);
    EXPECT_EQ(restored.s3Key, original.s3Key);
    EXPECT_EQ(restored.presignedUrl, original.presignedUrl);
    EXPECT_EQ(restored.fileSize, original.fileSize);
    EXPECT_EQ(restored.uploadedAt, original.uploadedAt);
    EXPECT_EQ(restored.expiresAt, original.expiresAt);
    EXPECT_EQ(restored.success, original.success);
}

TEST_F(UploadEntryTests, JsonRoundTrip_FailedEntry)
{
    UploadEntry original = UploadEntry::CreateFailure(
        L"failed.zip",
        L"E:\\failed.zip",
        L"Network error");

    nlohmann::json j = original.ToJson();
    UploadEntry restored = UploadEntry::FromJson(j);

    EXPECT_EQ(restored.fileName, original.fileName);
    EXPECT_EQ(restored.filePath, original.filePath);
    EXPECT_FALSE(restored.success);
    EXPECT_EQ(restored.errorMessage, original.errorMessage);
}

TEST_F(UploadEntryTests, FromJson_MissingFields)
{
    nlohmann::json j = {
        {"fileName", "test.txt"},
        {"success", true}
    };

    UploadEntry entry = UploadEntry::FromJson(j);

    EXPECT_EQ(entry.fileName, L"test.txt");
    EXPECT_TRUE(entry.success);
    EXPECT_EQ(entry.fileSize, 0);  // Default
    EXPECT_EQ(entry.uploadedAt, 0);  // Default
}
