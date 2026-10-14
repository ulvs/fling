#include "pch.h"
#include "S3Uploader.h"

#include <aws/core/auth/AWSCredentials.h>
#include <aws/core/client/ClientConfiguration.h>
#include <aws/core/client/DefaultRetryStrategy.h>
#include <aws/core/http/Scheme.h>
#include <aws/core/utils/UUID.h>
#include <aws/core/utils/memory/AWSMemory.h>
#include <aws/core/utils/memory/stl/AWSString.h>
#include <aws/core/utils/threading/Executor.h>
#include <aws/s3/S3Client.h>
#include <aws/s3/model/HeadBucketRequest.h>
#include <aws/s3/model/PutObjectRequest.h>
#include <aws/transfer/TransferHandle.h>
#include <aws/transfer/TransferManager.h>

#include <algorithm>
#include <cctype>
#include <fstream>
#include <thread>

using namespace Fling::Models;

namespace
{
    Aws::String ToAws(std::wstring const& w)
    {
        return Aws::String(WideToUtf8(w));
    }

    std::wstring FromAws(Aws::String const& a)
    {
        return Utf8ToWide(std::string(a.c_str(), a.size()));
    }

    Aws::String ContentTypeFor(std::filesystem::path const& path)
    {
        auto ext = path.extension().string();
        std::transform(ext.begin(), ext.end(), ext.begin(),
            [](unsigned char c) { return static_cast<char>(std::tolower(c)); });

        static const std::unordered_map<std::string, Aws::String> kMap = {
            { ".txt",  "text/plain" },
            { ".json", "application/json" },
            { ".xml",  "application/xml" },
            { ".html", "text/html" },
            { ".htm",  "text/html" },
            { ".css",  "text/css" },
            { ".js",   "application/javascript" },
            { ".pdf",  "application/pdf" },
            { ".zip",  "application/zip" },
            { ".7z",   "application/x-7z-compressed" },
            { ".tar",  "application/x-tar" },
            { ".gz",   "application/gzip" },
            { ".png",  "image/png" },
            { ".jpg",  "image/jpeg" },
            { ".jpeg", "image/jpeg" },
            { ".gif",  "image/gif" },
            { ".webp", "image/webp" },
            { ".svg",  "image/svg+xml" },
            { ".mp3",  "audio/mpeg" },
            { ".mp4",  "video/mp4" },
            { ".mov",  "video/quicktime" },
            { ".webm", "video/webm" },
            { ".doc",  "application/msword" },
            { ".docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document" },
            { ".xls",  "application/vnd.ms-excel" },
            { ".xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" },
            { ".ppt",  "application/vnd.ms-powerpoint" },
            { ".pptx", "application/vnd.openxmlformats-officedocument.presentationml.presentation" },
        };

        auto it = kMap.find(ext);
        return it != kMap.end() ? it->second : Aws::String("application/octet-stream");
    }

    Aws::String JoinKey(std::wstring const& prefix, std::wstring const& uuid, std::wstring const& fileName)
    {
        std::wstring key;
        if (!prefix.empty())
        {
            key = prefix;
            if (key.back() != L'/')
            {
                key.push_back(L'/');
            }
        }
        key += uuid;
        key.push_back(L'/');
        key += fileName;
        return ToAws(key);
    }
}

namespace Fling::Core
{
    S3Uploader::S3Uploader(FlingConfig const& config)
        : m_config(config)
    {
        // No EC2 metadata probing and few retries: the SDK defaults made an unreachable
        // endpoint take ~50 s to report failure.
        Aws::Client::ClientConfigurationInitValues initValues;
        initValues.shouldDisableIMDS = true;
        Aws::Client::ClientConfiguration clientConfig(initValues);
        clientConfig.connectTimeoutMs = 5000;
        clientConfig.retryStrategy = Aws::MakeShared<Aws::Client::DefaultRetryStrategy>("FlingS3Client", 2);
        // Optional CRC checksums (aws-chunked trailers) are rejected by Cloudflare R2 and some
        // older MinIO/Ceph builds; only send them when an operation requires one.
        clientConfig.checksumConfig.requestChecksumCalculation = Aws::Client::RequestChecksumCalculation::WHEN_REQUIRED;
        clientConfig.checksumConfig.responseChecksumValidation = Aws::Client::ResponseChecksumValidation::WHEN_REQUIRED;
        if (!m_config.region.empty())
        {
            clientConfig.region = ToAws(m_config.region);
        }

        if (!m_config.endpoint.empty())
        {
            auto endpoint = ToAws(m_config.endpoint);
            // endpointOverride is the host[:port], minus scheme.
            auto schemeEnd = endpoint.find("://");
            if (schemeEnd != Aws::String::npos)
            {
                auto scheme = endpoint.substr(0, schemeEnd);
                clientConfig.scheme = (scheme == "http")
                    ? Aws::Http::Scheme::HTTP
                    : Aws::Http::Scheme::HTTPS;
                clientConfig.endpointOverride = endpoint.substr(schemeEnd + 3);
            }
            else
            {
                clientConfig.endpointOverride = endpoint;
            }
        }

        Aws::Auth::AWSCredentials credentials(
            ToAws(m_config.accessKey),
            ToAws(m_config.secretKey));

        m_client = Aws::MakeShared<Aws::S3::S3Client>(
            "FlingS3Client",
            credentials,
            clientConfig,
            Aws::Client::AWSAuthV4Signer::PayloadSigningPolicy::Never,
            /*useVirtualAddressing=*/ !m_config.usePathStyle);
    }

    S3Uploader::~S3Uploader() = default;

    std::wstring S3Uploader::GenerateS3Key(std::wstring const& fileName)
    {
        auto uuid = Aws::Utils::UUID::RandomUUID();
        std::wstring uuidW = Utf8ToWide(std::string(Aws::String(uuid).c_str()));
        std::transform(uuidW.begin(), uuidW.end(), uuidW.begin(),
            [](wchar_t c) { return static_cast<wchar_t>(std::tolower(c)); });
        return FromAws(JoinKey(m_config.keyPrefix, uuidW, fileName));
    }

    UploadResult S3Uploader::Upload(
        std::filesystem::path const& filePath,
        ProgressCallback progressCallback)
    {
        m_cancelled = false;

        if (!std::filesystem::exists(filePath))
        {
            return UploadResult::Failure(L"File not found: " + filePath.wstring());
        }

        std::error_code ec;
        auto size = std::filesystem::file_size(filePath, ec);
        if (ec)
        {
            return UploadResult::Failure(L"Cannot read file size: " + Utf8ToWide(ec.message()));
        }

        auto key = GenerateS3Key(filePath.filename().wstring());

        auto result = (size >= LARGE_FILE_THRESHOLD)
            ? UploadLargeFile(filePath, key, progressCallback)
            : UploadSmallFile(filePath, key, progressCallback);

        if (result.success)
        {
            result.presignedUrl = GeneratePresignedUrl(key);
            if (result.presignedUrl.empty())
            {
                return UploadResult::Failure(L"Upload succeeded but presigned URL generation failed");
            }
        }
        return result;
    }

    UploadResult S3Uploader::UploadSmallFile(
        std::filesystem::path const& filePath,
        std::wstring const& s3Key,
        ProgressCallback progressCallback)
    {
        auto stream = Aws::MakeShared<Aws::FStream>(
            "FlingUploadStream",
            filePath.string().c_str(),
            std::ios::in | std::ios::binary);

        if (!stream || !stream->good())
        {
            return UploadResult::Failure(L"Failed to open file for reading");
        }

        Aws::S3::Model::PutObjectRequest request;
        request.SetBucket(ToAws(m_config.bucket));
        request.SetKey(ToAws(s3Key));
        request.SetContentType(ContentTypeFor(filePath));
        request.SetBody(stream);

        if (progressCallback)
        {
            request.SetDataSentEventHandler(
                [progressCallback, this](
                    Aws::Http::HttpRequest const*,
                    long long bytesSent)
                {
                    if (!m_cancelled)
                    {
                        progressCallback(static_cast<uint64_t>(bytesSent), 0);
                    }
                });
            request.SetContinueRequestHandler(
                [this](Aws::Http::HttpRequest const*) { return !m_cancelled.load(); });
        }

        auto outcome = m_client->PutObject(request);
        if (m_cancelled)
        {
            return UploadResult::Failure(L"Upload cancelled");
        }
        if (!outcome.IsSuccess())
        {
            return UploadResult::Failure(FromAws(outcome.GetError().GetMessage()));
        }
        return UploadResult::Success(L"", s3Key);
    }

    UploadResult S3Uploader::UploadLargeFile(
        std::filesystem::path const& filePath,
        std::wstring const& s3Key,
        ProgressCallback progressCallback)
    {
        auto executor = Aws::MakeShared<Aws::Utils::Threading::PooledThreadExecutor>(
            "FlingExecutor", std::max(2u, std::thread::hardware_concurrency()));

        Aws::Transfer::TransferManagerConfiguration transferConfig(executor.get());
        transferConfig.s3Client = m_client;
        // TransferManager sets CRC64NVME on every multipart request regardless of the client's
        // WHEN_REQUIRED setting; Backblaze B2 and Cloudflare R2 reject those headers.
        transferConfig.checksumAlgorithm = Aws::S3::Model::ChecksumAlgorithm::NOT_SET;

        if (progressCallback)
        {
            transferConfig.transferStatusUpdatedCallback =
                [progressCallback](
                    Aws::Transfer::TransferManager const*,
                    std::shared_ptr<Aws::Transfer::TransferHandle const> const& handle)
                {
                    progressCallback(handle->GetBytesTransferred(), handle->GetBytesTotalSize());
                };
        }

        auto transferManager = Aws::Transfer::TransferManager::Create(transferConfig);

        auto handle = transferManager->UploadFile(
            filePath.string().c_str(),
            ToAws(m_config.bucket),
            ToAws(s3Key),
            ContentTypeFor(filePath),
            Aws::Map<Aws::String, Aws::String>{});

        while (handle->GetStatus() != Aws::Transfer::TransferStatus::COMPLETED &&
               handle->GetStatus() != Aws::Transfer::TransferStatus::FAILED &&
               handle->GetStatus() != Aws::Transfer::TransferStatus::CANCELED)
        {
            if (m_cancelled.load())
            {
                handle->Cancel();
                transferManager->WaitUntilAllFinished();
                return UploadResult::Failure(L"Upload cancelled");
            }
            std::this_thread::sleep_for(std::chrono::milliseconds(100));
        }

        if (handle->GetStatus() != Aws::Transfer::TransferStatus::COMPLETED)
        {
            return UploadResult::Failure(FromAws(handle->GetLastError().GetMessage()));
        }
        return UploadResult::Success(L"", s3Key);
    }

    std::wstring S3Uploader::GeneratePresignedUrl(std::wstring const& s3Key)
    {
        if (!m_client)
        {
            return {};
        }
        auto url = m_client->GeneratePresignedUrl(
            ToAws(m_config.bucket),
            ToAws(s3Key),
            Aws::Http::HttpMethod::HTTP_GET,
            m_config.expirySeconds);
        return FromAws(url);
    }

    UploadResult S3Uploader::TestConnection()
    {
        if (!m_config.IsConfigured())
        {
            return UploadResult::Failure(L"Configuration incomplete");
        }

        Aws::S3::Model::HeadBucketRequest request;
        request.SetBucket(ToAws(m_config.bucket));

        auto outcome = m_client->HeadBucket(request);
        if (!outcome.IsSuccess())
        {
            return UploadResult::Failure(FromAws(outcome.GetError().GetMessage()));
        }
        return UploadResult::Success(L"", L"");
    }

    void S3Uploader::Cancel()
    {
        m_cancelled = true;
    }
}
