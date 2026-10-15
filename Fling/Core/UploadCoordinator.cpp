#include "pch.h"
#include "UploadCoordinator.h"

#include "ClipboardHelper.h"
#include "ConfigManager.h"
#include "HistoryManager.h"
#include "Log.h"

#include <chrono>
#include <iomanip>
#include <sstream>
#include "ToastHelper.h"

using namespace Fling::Models;

namespace Fling::Core
{
    UploadOutcome UploadCoordinator::Upload(std::filesystem::path const& filePath, ProgressCallback progress)
    {
        UploadOutcome out;

        if (!std::filesystem::exists(filePath))
        {
            out.errorMessage = L"File not found: " + filePath.wstring();
            Log::Error(L"Upload failed: the file no longer exists");
            return out;
        }

        ConfigManager cm;
        if (!cm.IsConfigured())
        {
            out.errorMessage = L"Fling is not configured. Open Settings to add your S3 credentials.";
            Log::Warning(L"Upload skipped: storage not configured");
            return out;
        }

        auto config = cm.Load();

        std::error_code ec;
        uint64_t fileSize = std::filesystem::file_size(filePath, ec);
        if (ec)
        {
            fileSize = 0;
        }

        Log::Info(L"Uploading " + Log::Describe(filePath, fileSize));
        auto started = std::chrono::steady_clock::now();
        S3Uploader uploader(config);
        auto result = uploader.Upload(filePath, std::move(progress));
        auto seconds = std::chrono::duration<double>(std::chrono::steady_clock::now() - started).count();
        if (result.success)
        {
            std::wostringstream os;
            os << L"Upload finished in " << std::fixed << std::setprecision(1) << seconds << L" s";
            Log::Info(os.str());
        }
        else
        {
            Log::Error(L"Upload failed after " + std::to_wstring(static_cast<int>(seconds)) + L" s: " + result.errorMessage);
        }

        auto fileName = filePath.filename().wstring();
        UploadEntry entry;
        if (result.success)
        {
            entry = UploadEntry::CreateSuccess(
                fileName,
                filePath.wstring(),
                result.s3Key,
                result.presignedUrl,
                fileSize,
                config.expirySeconds);
            out.success = true;
            out.url = result.presignedUrl;
        }
        else
        {
            entry = UploadEntry::CreateFailure(fileName, filePath.wstring(), result.errorMessage);
            out.errorMessage = result.errorMessage;
        }

        HistoryManager().AddEntry(entry);
        return out;
    }

    UploadOutcome UploadCoordinator::UploadAndShare(std::filesystem::path const& filePath)
    {
        auto out = Upload(filePath);
        auto fileName = filePath.filename().wstring();
        if (out.success)
        {
            ClipboardHelper::CopyToClipboard(out.url);
            ToastHelper::ShowUploadSuccess(fileName, out.url);
        }
        else
        {
            ToastHelper::ShowUploadError(fileName, out.errorMessage);
        }
        return out;
    }
}
