#include "pch.h"
#include "Log.h"

#include "ConfigManager.h"
#include "Format.h"
#include "../Models/FlingConfig.h"

#include <ShlObj.h>
#include <winrt/Windows.ApplicationModel.h>
#include <winrt/Windows.Storage.h>
#include <winrt/Windows.System.Profile.h>

#include <algorithm>
#include <chrono>
#include <cwctype>
#include <deque>
#include <fstream>
#include <iomanip>
#include <mutex>
#include <regex>
#include <sstream>

namespace fs = std::filesystem;
using Fling::Models::Utf8ToWide;
using Fling::Models::WideToUtf8;

namespace
{
    constexpr uintmax_t kMaxLogBytes = 1024 * 1024;
    constexpr wchar_t kLogName[] = L"fling.log";
    constexpr wchar_t kPreviousLogName[] = L"fling.1.log";

    std::mutex& LogMutex()
    {
        static std::mutex mutex;
        return mutex;
    }

    std::wstring Timestamp()
    {
        using namespace std::chrono;
        auto now = system_clock::now();
        auto ms = duration_cast<milliseconds>(now.time_since_epoch()).count() % 1000;
        std::time_t t = system_clock::to_time_t(now);
        std::tm tm{};
        localtime_s(&tm, &t);
        std::wostringstream os;
        os << std::put_time(&tm, L"%Y-%m-%d %H:%M:%S") << L'.' << std::setw(3) << std::setfill(L'0') << ms;
        return os.str();
    }

    void Write(wchar_t const* level, std::wstring const& message)
    {
        std::lock_guard lock(LogMutex());
        std::error_code ec;
        auto folder = Fling::Core::Log::Folder();
        fs::create_directories(folder, ec);
        auto file = folder / kLogName;
        if (fs::exists(file, ec) && fs::file_size(file, ec) > kMaxLogBytes)
        {
            fs::rename(file, folder / kPreviousLogName, ec);
        }

        std::ofstream out(file, std::ios::app | std::ios::binary);
        if (out)
        {
            out << WideToUtf8(Timestamp() + L"  " + level + L"  " + Fling::Core::Log::Scrub(message)) << "\r\n";
        }
    }

    std::wstring AppVersion()
    {
        try
        {
            auto v = winrt::Windows::ApplicationModel::Package::Current().Id().Version();
            return std::to_wstring(v.Major) + L"." + std::to_wstring(v.Minor) + L"." + std::to_wstring(v.Build) + L"." + std::to_wstring(v.Revision);
        }
        catch (winrt::hresult_error const&)
        {
            return L"unpackaged build";
        }
    }

    std::wstring WindowsVersion()
    {
        auto raw = std::stoull(std::wstring(winrt::Windows::System::Profile::AnalyticsInfo::VersionInfo().DeviceFamilyVersion()));
        return std::to_wstring((raw >> 48) & 0xFFFF) + L"." + std::to_wstring((raw >> 32) & 0xFFFF) + L"."
            + std::to_wstring((raw >> 16) & 0xFFFF) + L"." + std::to_wstring(raw & 0xFFFF);
    }

    std::wstring EndpointKind(std::wstring const& endpoint)
    {
        std::wstring lower = endpoint;
        std::transform(lower.begin(), lower.end(), lower.begin(), [](wchar_t c) { return static_cast<wchar_t>(std::towlower(c)); });
        std::wstring scheme = lower.starts_with(L"http://") ? L"HTTP" : L"HTTPS";
        if (lower.find(L"backblazeb2.com") != std::wstring::npos) return L"Backblaze B2 (" + scheme + L")";
        if (lower.find(L"r2.cloudflarestorage.com") != std::wstring::npos) return L"Cloudflare R2 (" + scheme + L")";
        if (lower.find(L"amazonaws.com") != std::wstring::npos) return L"Amazon S3 (" + scheme + L")";
        if (lower.empty()) return L"not set";
        return L"other S3-compatible (" + scheme + L")";
    }

    std::deque<std::wstring> LastLines(fs::path const& file, size_t count)
    {
        std::deque<std::wstring> lines;
        std::ifstream in(file, std::ios::binary);
        std::string line;
        while (std::getline(in, line))
        {
            if (!line.empty() && line.back() == '\r')
            {
                line.pop_back();
            }
            lines.push_back(Utf8ToWide(line));
            if (lines.size() > count)
            {
                lines.pop_front();
            }
        }
        return lines;
    }
}

namespace Fling::Core
{
    void Log::Info(std::wstring const& message) { Write(L"INFO ", message); }
    void Log::Warning(std::wstring const& message) { Write(L"WARN ", message); }
    void Log::Error(std::wstring const& message) { Write(L"ERROR", message); }

    fs::path Log::Folder()
    {
        static const fs::path folder = []
        {
            // Packaged: the package's real LocalState folder, which Explorer can open.
            try
            {
                return fs::path(std::wstring(winrt::Windows::Storage::ApplicationData::Current().LocalFolder().Path())) / L"logs";
            }
            catch (winrt::hresult_error const&)
            {
            }
            PWSTR localAppData{};
            fs::path base;
            if (SUCCEEDED(SHGetKnownFolderPath(FOLDERID_LocalAppData, 0, nullptr, &localAppData)))
            {
                base = localAppData;
                CoTaskMemFree(localAppData);
            }
            return base / L"Fling" / L"logs";
        }();
        return folder;
    }

    std::wstring Log::Scrub(std::wstring const& text)
    {
        // Presigned URLs carry their signature in the query string; drop every query string.
        static const std::wregex urlQuery(LR"((https?://[^\s?"']+)\?[^\s"']*)", std::regex::icase);
        std::wstring result = std::regex_replace(text, urlQuery, L"$1?\u2026");

        wchar_t profile[MAX_PATH]{};
        DWORD length = GetEnvironmentVariableW(L"USERPROFILE", profile, MAX_PATH);
        if (length > 0 && length < MAX_PATH)
        {
            std::wstring needle(profile, length);
            for (size_t pos = 0; (pos = result.find(needle, pos)) != std::wstring::npos;)
            {
                result.replace(pos, needle.size(), L"%USERPROFILE%");
                pos += 13;
            }
        }
        return result;
    }

    std::wstring Log::DescribeType(fs::path const& file)
    {
        auto ext = file.extension().wstring();
        return ext.empty() ? std::wstring(L"a file without extension") : L"a " + ext + L" file";
    }

    std::wstring Log::Describe(fs::path const& file, uint64_t size)
    {
        return DescribeType(file) + L" (" + FormatSize(size) + L")";
    }

    std::wstring Log::DiagnosticInfo(size_t lastLines)
    {
        auto config = ConfigManager().Load();
        std::wostringstream os;
        os << L"Fling " << AppVersion() << L" (x64)\r\n";
        os << L"Windows " << WindowsVersion() << L"\r\n";
        os << L"Storage: " << (config.IsConfigured() ? L"configured" : L"not configured")
           << L", " << EndpointKind(config.endpoint)
           << L", region " << (config.region.empty() ? L"empty" : L"set")
           << L", path-style " << (config.UsesPathStyle() ? L"on" : L"off")
           << L", links expire after " << config.expirySeconds / 86400 << L" days\r\n";
        os << L"Several items: " << (config.zipMultiple ? L"one zip" : L"separate links") << L"\r\n";
        os << L"\r\nLast log lines:\r\n";

        std::lock_guard lock(LogMutex());
        for (auto const& line : LastLines(Folder() / kLogName, lastLines))
        {
            os << line << L"\r\n";
        }
        return os.str();
    }
}
