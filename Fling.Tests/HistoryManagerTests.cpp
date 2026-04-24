#include "pch.h"
#include "TestHelpers.h"
#include "Core/HistoryManager.h"

using namespace Fling::Core;
using namespace Fling::Models;

class HistoryManagerTests : public ::testing::Test
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

TEST_F(HistoryManagerTests, LoadEntries_EmptyFile)
{
    auto historyPath = m_testDir / L"history.json";
    HistoryManager manager(historyPath);

    auto entries = manager.LoadEntries();

    EXPECT_TRUE(entries.empty());
}

TEST_F(HistoryManagerTests, AddEntry_CreatesFile)
{
    auto historyPath = m_testDir / L"history.json";
    HistoryManager manager(historyPath);

    auto entry = UploadEntry::CreateSuccess(
        L"test.txt",
        L"C:\\test.txt",
        L"key",
        L"https://example.com",
        100,
        3600);

    manager.AddEntry(entry);

    EXPECT_TRUE(std::filesystem::exists(historyPath));
}

TEST_F(HistoryManagerTests, AddEntry_AppendsToBeginning)
{
    auto historyPath = m_testDir / L"history.json";
    HistoryManager manager(historyPath);

    auto entry1 = UploadEntry::CreateSuccess(L"first.txt", L"C:\\first.txt", L"k1", L"url1", 100, 3600);
    auto entry2 = UploadEntry::CreateSuccess(L"second.txt", L"C:\\second.txt", L"k2", L"url2", 200, 3600);

    manager.AddEntry(entry1);
    manager.AddEntry(entry2);

    auto entries = manager.LoadEntries();

    EXPECT_EQ(entries.size(), 2);
    EXPECT_EQ(entries[0].fileName, L"second.txt");  // Most recent first
    EXPECT_EQ(entries[1].fileName, L"first.txt");
}

TEST_F(HistoryManagerTests, AddEntry_EnforcesMaxEntries)
{
    auto historyPath = m_testDir / L"history.json";
    HistoryManager manager(historyPath);

    // Add more than MAX_ENTRIES
    for (size_t i = 0; i < HistoryManager::MAX_ENTRIES + 10; ++i)
    {
        auto entry = UploadEntry::CreateSuccess(
            L"file" + std::to_wstring(i) + L".txt",
            L"C:\\file.txt",
            L"key",
            L"url",
            100,
            3600);
        manager.AddEntry(entry);
    }

    auto entries = manager.LoadEntries();

    EXPECT_EQ(entries.size(), HistoryManager::MAX_ENTRIES);
    // Most recent should be at the beginning
    EXPECT_EQ(entries[0].fileName, L"file" + std::to_wstring(HistoryManager::MAX_ENTRIES + 9) + L".txt");
}

TEST_F(HistoryManagerTests, ClearAll)
{
    auto historyPath = m_testDir / L"history.json";
    HistoryManager manager(historyPath);

    auto entry = UploadEntry::CreateSuccess(L"test.txt", L"C:\\test.txt", L"k", L"url", 100, 3600);
    manager.AddEntry(entry);

    EXPECT_EQ(manager.GetEntryCount(), 1);

    manager.ClearAll();

    EXPECT_EQ(manager.GetEntryCount(), 0);
    auto entries = manager.LoadEntries();
    EXPECT_TRUE(entries.empty());
}

TEST_F(HistoryManagerTests, RemoveExpired)
{
    auto historyPath = m_testDir / L"history.json";
    HistoryManager manager(historyPath);

    // Add expired entry
    UploadEntry expired;
    expired.fileName = L"expired.txt";
    expired.uploadedAt = 1;  // Long ago
    expired.expiresAt = 2;   // Also long ago
    expired.success = true;
    manager.AddEntry(expired);

    // Add non-expired entry
    auto valid = UploadEntry::CreateSuccess(L"valid.txt", L"C:\\valid.txt", L"k", L"url", 100, 604800);
    manager.AddEntry(valid);

    EXPECT_EQ(manager.GetEntryCount(), 2);

    manager.RemoveExpired();

    EXPECT_EQ(manager.GetEntryCount(), 1);
    auto entries = manager.LoadEntries();
    EXPECT_EQ(entries[0].fileName, L"valid.txt");
}

TEST_F(HistoryManagerTests, GetEntryCount)
{
    auto historyPath = m_testDir / L"history.json";
    HistoryManager manager(historyPath);

    EXPECT_EQ(manager.GetEntryCount(), 0);

    auto entry = UploadEntry::CreateSuccess(L"test.txt", L"C:\\test.txt", L"k", L"url", 100, 3600);
    manager.AddEntry(entry);

    EXPECT_EQ(manager.GetEntryCount(), 1);

    manager.AddEntry(entry);
    manager.AddEntry(entry);

    EXPECT_EQ(manager.GetEntryCount(), 3);
}

TEST_F(HistoryManagerTests, GetHistoryPath)
{
    auto historyPath = m_testDir / L"history.json";
    HistoryManager manager(historyPath);

    EXPECT_EQ(manager.GetHistoryPath(), historyPath);
}

TEST_F(HistoryManagerTests, LoadEntries_CorruptedJson)
{
    auto historyPath = m_testDir / L"history.json";

    // Write corrupted JSON
    std::ofstream file(historyPath);
    file << "{ not valid array }}";
    file.close();

    HistoryManager manager(historyPath);
    auto entries = manager.LoadEntries();

    EXPECT_TRUE(entries.empty());
}

TEST_F(HistoryManagerTests, LoadEntries_NotAnArray)
{
    auto historyPath = m_testDir / L"history.json";

    // Write JSON object instead of array
    std::ofstream file(historyPath);
    file << R"({"key": "value"})";
    file.close();

    HistoryManager manager(historyPath);
    auto entries = manager.LoadEntries();

    EXPECT_TRUE(entries.empty());
}

TEST_F(HistoryManagerTests, Save_CreatesDirectories)
{
    auto historyPath = m_testDir / L"nested" / L"dir" / L"history.json";
    HistoryManager manager(historyPath);

    auto entry = UploadEntry::CreateSuccess(L"test.txt", L"C:\\test.txt", L"k", L"url", 100, 3600);
    manager.AddEntry(entry);

    EXPECT_TRUE(std::filesystem::exists(historyPath));
}
