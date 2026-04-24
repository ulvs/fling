#pragma once
#include "../Models/FlingConfig.h"
#include "../Models/UploadEntry.h"
#include <atomic>
#include <filesystem>
#include <functional>
#include <memory>

namespace Aws::S3 { class S3Client; }

namespace Fling::Core
{
    // Progress callback: (bytesTransferred, totalBytes) -> void
    using ProgressCallback = std::function<void(uint64_t, uint64_t)>;

    struct UploadResult
    {
        bool success{ false };
        std::wstring presignedUrl;
        std::wstring s3Key;
        std::wstring errorMessage;

        static UploadResult Success(std::wstring const& url, std::wstring const& key)
        {
            UploadResult result;
            result.success = true;
            result.presignedUrl = url;
            result.s3Key = key;
            return result;
        }

        static UploadResult Failure(std::wstring const& error)
        {
            UploadResult result;
            result.success = false;
            result.errorMessage = error;
            return result;
        }
    };

    class S3Uploader
    {
    public:
        explicit S3Uploader(Models::FlingConfig const& config);
        ~S3Uploader();

        // Upload a file to S3
        // Returns result with presigned URL on success
        UploadResult Upload(
            std::filesystem::path const& filePath,
            ProgressCallback progressCallback = nullptr);

        // Test connection to S3 (HeadBucket operation)
        UploadResult TestConnection();

        // Generate presigned URL for existing object
        std::wstring GeneratePresignedUrl(std::wstring const& s3Key);

        // Cancel ongoing upload
        void Cancel();

    private:
        Models::FlingConfig m_config;
        std::shared_ptr<Aws::S3::S3Client> m_client;
        std::atomic<bool> m_cancelled{ false };

        // Generate unique S3 key for file
        std::wstring GenerateS3Key(std::wstring const& fileName);

        // Use TransferManager for large files (>100MB)
        UploadResult UploadLargeFile(
            std::filesystem::path const& filePath,
            std::wstring const& s3Key,
            ProgressCallback progressCallback);

        // Use simple PutObject for smaller files
        UploadResult UploadSmallFile(
            std::filesystem::path const& filePath,
            std::wstring const& s3Key,
            ProgressCallback progressCallback);

        static constexpr uint64_t LARGE_FILE_THRESHOLD = 100 * 1024 * 1024; // 100MB
    };
}
