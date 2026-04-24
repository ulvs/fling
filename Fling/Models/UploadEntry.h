#pragma once
#include <string>
#include <chrono>
#include <nlohmann/json.hpp>
#include "FlingConfig.h"

namespace Fling::Models
{
    struct UploadEntry
    {
        std::wstring fileName;          // Original file name
        std::wstring filePath;          // Full local path
        std::wstring s3Key;             // S3 object key
        std::wstring presignedUrl;      // Presigned download URL
        uint64_t fileSize{ 0 };         // File size in bytes
        int64_t uploadedAt{ 0 };        // Unix timestamp (seconds since epoch)
        int64_t expiresAt{ 0 };         // Unix timestamp when presigned URL expires
        bool success{ true };           // Upload success status
        std::wstring errorMessage;      // Error message if failed

        nlohmann::json ToJson() const
        {
            return nlohmann::json{
                {"fileName", WideToUtf8(fileName)},
                {"filePath", WideToUtf8(filePath)},
                {"s3Key", WideToUtf8(s3Key)},
                {"presignedUrl", WideToUtf8(presignedUrl)},
                {"fileSize", fileSize},
                {"uploadedAt", uploadedAt},
                {"expiresAt", expiresAt},
                {"success", success},
                {"errorMessage", WideToUtf8(errorMessage)}
            };
        }

        static UploadEntry FromJson(nlohmann::json const& j)
        {
            UploadEntry entry;

            if (j.contains("fileName") && j["fileName"].is_string())
                entry.fileName = Utf8ToWide(j["fileName"].get<std::string>());

            if (j.contains("filePath") && j["filePath"].is_string())
                entry.filePath = Utf8ToWide(j["filePath"].get<std::string>());

            if (j.contains("s3Key") && j["s3Key"].is_string())
                entry.s3Key = Utf8ToWide(j["s3Key"].get<std::string>());

            if (j.contains("presignedUrl") && j["presignedUrl"].is_string())
                entry.presignedUrl = Utf8ToWide(j["presignedUrl"].get<std::string>());

            if (j.contains("fileSize") && j["fileSize"].is_number_unsigned())
                entry.fileSize = j["fileSize"].get<uint64_t>();

            if (j.contains("uploadedAt") && j["uploadedAt"].is_number_integer())
                entry.uploadedAt = j["uploadedAt"].get<int64_t>();

            if (j.contains("expiresAt") && j["expiresAt"].is_number_integer())
                entry.expiresAt = j["expiresAt"].get<int64_t>();

            if (j.contains("success") && j["success"].is_boolean())
                entry.success = j["success"].get<bool>();

            if (j.contains("errorMessage") && j["errorMessage"].is_string())
                entry.errorMessage = Utf8ToWide(j["errorMessage"].get<std::string>());

            return entry;
        }

        // Check if the presigned URL has expired
        bool IsExpired() const
        {
            auto now = std::chrono::system_clock::now();
            auto nowSeconds = std::chrono::duration_cast<std::chrono::seconds>(
                now.time_since_epoch()).count();
            return nowSeconds > expiresAt;
        }

        // Create entry for successful upload
        static UploadEntry CreateSuccess(
            std::wstring const& fileName,
            std::wstring const& filePath,
            std::wstring const& s3Key,
            std::wstring const& presignedUrl,
            uint64_t fileSize,
            int expirySeconds)
        {
            UploadEntry entry;
            entry.fileName = fileName;
            entry.filePath = filePath;
            entry.s3Key = s3Key;
            entry.presignedUrl = presignedUrl;
            entry.fileSize = fileSize;
            entry.success = true;

            auto now = std::chrono::system_clock::now();
            entry.uploadedAt = std::chrono::duration_cast<std::chrono::seconds>(
                now.time_since_epoch()).count();
            entry.expiresAt = entry.uploadedAt + expirySeconds;

            return entry;
        }

        // Create entry for failed upload
        static UploadEntry CreateFailure(
            std::wstring const& fileName,
            std::wstring const& filePath,
            std::wstring const& errorMessage)
        {
            UploadEntry entry;
            entry.fileName = fileName;
            entry.filePath = filePath;
            entry.success = false;
            entry.errorMessage = errorMessage;

            auto now = std::chrono::system_clock::now();
            entry.uploadedAt = std::chrono::duration_cast<std::chrono::seconds>(
                now.time_since_epoch()).count();

            return entry;
        }
    };
}
