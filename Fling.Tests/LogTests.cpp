#include "pch.h"
#include "Core/Log.h"

using Fling::Core::Log;

TEST(LogTests, Scrub_RemovesPresignedQueryStrings)
{
    std::wstring message = L"GET https://s3.example.com/bucket/key.pdf?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Signature=abc123 failed";
    auto scrubbed = Log::Scrub(message);
    EXPECT_EQ(scrubbed.find(L"X-Amz-Signature"), std::wstring::npos);
    EXPECT_EQ(scrubbed.find(L"abc123"), std::wstring::npos);
    EXPECT_NE(scrubbed.find(L"https://s3.example.com/bucket/key.pdf?"), std::wstring::npos);
    EXPECT_NE(scrubbed.find(L" failed"), std::wstring::npos);
}

TEST(LogTests, Scrub_ReplacesUserProfilePath)
{
    wchar_t profile[MAX_PATH]{};
    ASSERT_GT(GetEnvironmentVariableW(L"USERPROFILE", profile, MAX_PATH), 0u);
    auto scrubbed = Log::Scrub(std::wstring(L"Couldn't open ") + profile + L"\\Documents\\x.txt");
    EXPECT_EQ(scrubbed, L"Couldn't open %USERPROFILE%\\Documents\\x.txt");
}

TEST(LogTests, Scrub_LeavesPlainTextAlone)
{
    EXPECT_EQ(Log::Scrub(L"Upload finished in 1.2 s"), L"Upload finished in 1.2 s");
}

TEST(LogTests, Describe_UsesExtensionAndSizeNotName)
{
    auto text = Log::Describe(L"C:\\secret\\Salary review 2026.pdf", 2516582);
    EXPECT_EQ(text, L"a .pdf file (2.4 MB)");
    EXPECT_EQ(Log::Describe(L"C:\\x\\README", 10), L"a file without extension (10 B)");
}
