#include "pch.h"
#include "UploadCoordinator.h"

#include "ClipboardHelper.h"
#include "ConfigManager.h"
#include "HistoryManager.h"
#include "S3Uploader.h"
#include "ToastHelper.h"

using namespace Fling::Models;

namespace Fling::Core
{
    UploadOutcome UploadCoordinator::UploadAndShare(std::filesystem::path const& filePath)
    {
        UploadOutcome out;
        auto fileName = filePath.filename().wstring();

        if (!std::filesystem::exists(filePath))
        {
            out.errorMessage = L"File not found: " + filePath.wstring();
            ToastHelper::ShowUploadError(fileName, out.errorMessage);
            return out;
        }

        ConfigManager cm;
        if (!cm.IsConfigured())
        {
            out.errorMessage = L"Fling is not configured. Open Settings to add your S3 credentials.";
            ToastHelper::ShowUploadError(fileName, out.errorMessage);
            return out;
        }

        auto config = cm.Load();

        std::error_code ec;
        uint64_t fileSize = std::filesystem::file_size(filePath, ec);
        if (ec)
        {
            fileSize = 0;
        }

        S3Uploader uploader(config);
        auto result = uploader.Upload(filePath);

        HistoryManager hm;
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

            ClipboardHelper::CopyToClipboard(result.presignedUrl);
            ToastHelper::ShowUploadSuccess(fileName, result.presignedUrl);

            out.success = true;
            out.url = result.presignedUrl;
        }
        else
        {
            entry = UploadEntry::CreateFailure(
                fileName,
                filePath.wstring(),
                result.errorMessage);

            ToastHelper::ShowUploadError(fileName, result.errorMessage);
            out.errorMessage = result.errorMessage;
        }

        hm.AddEntry(entry);
        return out;
    }
}
