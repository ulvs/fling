#include "pch.h"
#include "ArchiveBuilder.h"

#include "../Models/FlingConfig.h"

#include <minizip-ng/mz.h>
#include <minizip-ng/mz_strm.h>
#include <minizip-ng/mz_zip.h>
#include <minizip-ng/mz_zip_rw.h>

#include <algorithm>
#include <chrono>
#include <cwctype>
#include <iomanip>
#include <set>
#include <sstream>

namespace fs = std::filesystem;
using Fling::Models::WideToUtf8;

namespace
{
    constexpr wchar_t kTempFolderName[] = L"Fling";

    bool IsAlreadyCompressed(fs::path const& file)
    {
        static const std::set<std::wstring> kStored = {
            L".zip", L".7z", L".rar", L".gz", L".tgz", L".bz2", L".xz", L".zst", L".cab",
            L".jpg", L".jpeg", L".png", L".gif", L".webp", L".heic", L".heif", L".avif",
            L".mp4", L".mov", L".mkv", L".avi", L".webm", L".m4v", L".wmv",
            L".mp3", L".m4a", L".aac", L".ogg", L".opus", L".flac", L".wma",
            L".docx", L".xlsx", L".pptx", L".odt", L".ods", L".odp", L".epub", L".msix", L".appx",
        };
        auto ext = file.extension().wstring();
        std::transform(ext.begin(), ext.end(), ext.begin(), [](wchar_t c) { return static_cast<wchar_t>(std::towlower(c)); });
        return kStored.contains(ext);
    }

    // Unique top-level name inside the archive: "a.txt", then "a (2).txt", ...
    std::wstring UniqueName(std::wstring const& name, std::set<std::wstring>& used)
    {
        fs::path p(name);
        std::wstring candidate = name;
        for (int n = 2; used.contains(candidate); ++n)
        {
            candidate = p.stem().wstring() + L" (" + std::to_wstring(n) + L")" + p.extension().wstring();
        }
        used.insert(candidate);
        return candidate;
    }

    struct Entry
    {
        fs::path source;
        std::wstring nameInZip;
        uint64_t size{ 0 };
        bool directory{ false };
    };

    std::vector<Entry> CollectEntries(std::vector<fs::path> const& items)
    {
        std::vector<Entry> entries;
        std::set<std::wstring> usedTopLevel;
        std::error_code ec;
        for (auto const& item : items)
        {
            auto root = UniqueName(item.filename().wstring(), usedTopLevel);
            if (fs::is_regular_file(item, ec))
            {
                entries.push_back({ item, root, fs::file_size(item, ec), false });
                continue;
            }
            if (!fs::is_directory(item, ec))
            {
                continue;
            }

            bool empty = true;
            for (auto it = fs::recursive_directory_iterator(item, fs::directory_options::skip_permission_denied, ec);
                 it != fs::recursive_directory_iterator(); it.increment(ec))
            {
                if (ec)
                {
                    break;
                }
                empty = false;
                auto relative = fs::relative(it->path(), item, ec).generic_wstring();
                std::wstring name = root + L"/" + relative;
                if (it->is_directory(ec))
                {
                    if (fs::is_empty(it->path(), ec))
                    {
                        entries.push_back({ it->path(), name + L"/", 0, true });
                    }
                }
                else if (it->is_regular_file(ec))
                {
                    entries.push_back({ it->path(), name, it->file_size(ec), false });
                }
            }
            if (empty)
            {
                entries.push_back({ item, root + L"/", 0, true });
            }
        }
        return entries;
    }

    struct ProgressState
    {
        std::function<void(uint64_t, uint64_t)> callback;
        uint64_t doneBefore{ 0 };
        uint64_t total{ 0 };
    };

    int32_t OnWriterProgress(void*, void* userdata, mz_zip_file*, int64_t position)
    {
        auto state = static_cast<ProgressState*>(userdata);
        if (state->callback && position >= 0)
        {
            state->callback(state->doneBefore + static_cast<uint64_t>(position), state->total);
        }
        return MZ_OK;
    }
}

namespace Fling::Core
{
    std::wstring ArchiveBuilder::ArchiveName(std::vector<fs::path> const& items, std::time_t when)
    {
        std::error_code ec;
        if (items.size() == 1 && fs::is_directory(items[0], ec) && !items[0].filename().empty())
        {
            return items[0].filename().wstring() + L".zip";
        }
        std::tm tm{};
        localtime_s(&tm, &when);
        std::wostringstream os;
        os << L"flung-" << std::put_time(&tm, L"%Y-%m-%d-%H%M") << L".zip";
        return os.str();
    }

    uint64_t ArchiveBuilder::TotalSize(std::vector<fs::path> const& items)
    {
        uint64_t total = 0;
        for (auto const& entry : CollectEntries(items))
        {
            total += entry.size;
        }
        return total;
    }

    ArchiveResult ArchiveBuilder::Build(std::vector<fs::path> const& items,
                                        std::wstring const& archiveName,
                                        std::function<void(uint64_t, uint64_t)> progress)
    {
        ArchiveResult result;
        auto entries = CollectEntries(items);
        if (entries.empty())
        {
            result.errorMessage = L"There was nothing to zip.";
            return result;
        }

        std::error_code ec;
        auto folder = fs::temp_directory_path(ec) / kTempFolderName /
            std::to_wstring(std::chrono::steady_clock::now().time_since_epoch().count());
        fs::create_directories(folder, ec);
        if (ec)
        {
            result.errorMessage = L"Couldn't create a temporary folder for the zip file.";
            return result;
        }
        result.path = folder / archiveName;

        ProgressState state{ std::move(progress) };
        for (auto const& entry : entries)
        {
            state.total += entry.size;
        }

        void* writer = mz_zip_writer_create();
        mz_zip_writer_set_compress_level(writer, MZ_COMPRESS_LEVEL_DEFAULT);
        mz_zip_writer_set_follow_links(writer, 0);
        mz_zip_writer_set_progress_cb(writer, &state, OnWriterProgress);
        mz_zip_writer_set_progress_interval(writer, 200);

        int32_t err = mz_zip_writer_open_file(writer, WideToUtf8(result.path.wstring()).c_str(), 0, 0);
        for (size_t i = 0; err == MZ_OK && i < entries.size(); ++i)
        {
            auto const& entry = entries[i];
            mz_zip_writer_set_compress_method(writer,
                entry.directory || IsAlreadyCompressed(entry.source) ? MZ_COMPRESS_METHOD_STORE : MZ_COMPRESS_METHOD_DEFLATE);
            err = mz_zip_writer_add_file(writer, WideToUtf8(entry.source.wstring()).c_str(), WideToUtf8(entry.nameInZip).c_str());
            if (err != MZ_OK)
            {
                result.errorMessage = L"Couldn't add " + entry.source.filename().wstring() + L" to the zip file.";
                result.failedFile = entry.source;
                break;
            }
            state.doneBefore += entry.size;
            if (state.callback)
            {
                state.callback(state.doneBefore, state.total);
            }
        }

        int32_t closeErr = mz_zip_writer_close(writer);
        mz_zip_writer_delete(&writer);

        if (err == MZ_OK && closeErr != MZ_OK)
        {
            err = closeErr;
            result.errorMessage = L"Couldn't finish the zip file. The disk may be full.";
        }
        if (err != MZ_OK)
        {
            if (result.errorMessage.empty())
            {
                result.errorMessage = L"Couldn't create the zip file.";
            }
            Cleanup(result.path);
            result.path.clear();
            return result;
        }

        result.success = true;
        return result;
    }

    void ArchiveBuilder::Cleanup(fs::path const& archivePath)
    {
        std::error_code ec;
        auto folder = archivePath.parent_path();
        // Only ever delete the per-archive folder Build created under %TEMP%\Fling.
        if (!folder.empty() && folder.parent_path().filename() == kTempFolderName)
        {
            fs::remove_all(folder, ec);
        }
    }
}
