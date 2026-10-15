#include "pch.h"
#include "TestHelpers.h"
#include "Core/ArchiveBuilder.h"
#include "Models/FlingConfig.h"

#include <minizip-ng/mz.h>
#include <minizip-ng/mz_strm.h>
#include <minizip-ng/mz_zip.h>
#include <minizip-ng/mz_zip_rw.h>

#include <map>

using namespace Fling::Core;
namespace fs = std::filesystem;

namespace
{
    // name in zip -> compression method
    std::map<std::string, uint16_t> ReadEntries(fs::path const& zip)
    {
        std::map<std::string, uint16_t> entries;
        void* reader = mz_zip_reader_create();
        if (mz_zip_reader_open_file(reader, Fling::Models::WideToUtf8(zip.wstring()).c_str()) == MZ_OK)
        {
            for (int32_t err = mz_zip_reader_goto_first_entry(reader); err == MZ_OK; err = mz_zip_reader_goto_next_entry(reader))
            {
                mz_zip_file* info = nullptr;
                if (mz_zip_reader_entry_get_info(reader, &info) == MZ_OK)
                {
                    entries[info->filename] = info->compression_method;
                }
            }
            mz_zip_reader_close(reader);
        }
        mz_zip_reader_delete(&reader);
        return entries;
    }
}

class ArchiveBuilderTests : public ::testing::Test
{
protected:
    fs::path m_dir;
    void SetUp() override { m_dir = TestHelpers::CreateTempDirectory(); }
    void TearDown() override { TestHelpers::CleanupTempDirectory(m_dir); }
};

TEST_F(ArchiveBuilderTests, ArchiveName_SingleFolderUsesFolderName)
{
    auto folder = m_dir / L"Holiday photos";
    fs::create_directories(folder);
    EXPECT_EQ(ArchiveBuilder::ArchiveName({ folder }), L"Holiday photos.zip");
}

TEST_F(ArchiveBuilderTests, ArchiveName_SeveralItemsUseFlungDateTime)
{
    auto a = TestHelpers::CreateTestFile(m_dir, L"a.txt", 10);
    auto b = TestHelpers::CreateTestFile(m_dir, L"b.txt", 10);
    std::tm tm{};
    tm.tm_year = 2026 - 1900;
    tm.tm_mon = 9;
    tm.tm_mday = 15;
    tm.tm_hour = 9;
    tm.tm_min = 30;
    tm.tm_isdst = -1;
    EXPECT_EQ(ArchiveBuilder::ArchiveName({ a, b }, std::mktime(&tm)), L"flung-2026-10-15-0930.zip");
}

TEST_F(ArchiveBuilderTests, TotalSize_IncludesNestedFiles)
{
    auto folder = m_dir / L"docs";
    fs::create_directories(folder / L"deep");
    TestHelpers::CreateTestFile(folder, L"one.bin", 100);
    TestHelpers::CreateTestFile(folder / L"deep", L"two.bin", 250);
    auto loose = TestHelpers::CreateTestFile(m_dir, L"three.bin", 50);
    EXPECT_EQ(ArchiveBuilder::TotalSize({ folder, loose }), 400u);
}

TEST_F(ArchiveBuilderTests, Build_ZipsFoldersAndFilesWithExpectedEntries)
{
    auto folder = m_dir / L"Project";
    fs::create_directories(folder / L"src");
    fs::create_directories(folder / L"empty");
    TestHelpers::CreateTestFile(folder / L"src", L"main.cpp", 2000);
    TestHelpers::CreateTestFile(folder, L"photo.JPG", 2000);
    auto loose = TestHelpers::CreateTestFile(m_dir, L"r\u00E4ksm\u00F6rg\u00E5s.txt", 300);

    // A second file with the same name from another folder must not overwrite the first.
    fs::create_directories(m_dir / L"other");
    auto duplicate = TestHelpers::CreateTestFile(m_dir / L"other", L"r\u00E4ksm\u00F6rg\u00E5s.txt", 300);

    std::vector<uint64_t> progress;
    auto result = ArchiveBuilder::Build({ folder, loose, duplicate }, L"flung-test.zip",
        [&](uint64_t done, uint64_t) { progress.push_back(done); });

    ASSERT_TRUE(result.success) << Fling::Models::WideToUtf8(result.errorMessage);
    EXPECT_EQ(result.path.filename(), L"flung-test.zip");
    auto entries = ReadEntries(result.path);

    EXPECT_EQ(entries.count("Project/src/main.cpp"), 1u);
    EXPECT_EQ(entries.count("Project/empty/"), 1u);
    EXPECT_EQ(entries.count(Fling::Models::WideToUtf8(L"r\u00E4ksm\u00F6rg\u00E5s.txt")), 1u);
    EXPECT_EQ(entries.count(Fling::Models::WideToUtf8(L"r\u00E4ksm\u00F6rg\u00E5s (2).txt")), 1u);
    EXPECT_EQ(entries["Project/photo.JPG"], MZ_COMPRESS_METHOD_STORE);
    EXPECT_EQ(entries["Project/src/main.cpp"], MZ_COMPRESS_METHOD_DEFLATE);
    ASSERT_FALSE(progress.empty());
    EXPECT_EQ(progress.back(), 4600u);

    ArchiveBuilder::Cleanup(result.path);
    EXPECT_FALSE(fs::exists(result.path.parent_path()));
}

TEST_F(ArchiveBuilderTests, Build_NothingToZipFails)
{
    auto result = ArchiveBuilder::Build({ m_dir / L"missing" }, L"x.zip");
    EXPECT_FALSE(result.success);
    EXPECT_FALSE(result.errorMessage.empty());
}
