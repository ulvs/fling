#pragma once
#include <string>
#include <nlohmann/json.hpp>

namespace Fling::Models
{
    struct FlingConfig
    {
        std::wstring endpoint;          // S3 endpoint URL (e.g., "https://s3.amazonaws.com" or "http://localhost:9000")
        std::wstring region;            // AWS region (e.g., "us-east-1")
        std::wstring bucket;            // S3 bucket name
        std::wstring accessKey;         // AWS access key ID
        std::wstring secretKey;         // AWS secret access key
        int expirySeconds{ 604800 };    // Presigned URL expiry (default 7 days)
        bool usePathStyle{ false };     // Use path-style addressing (required for MinIO)
        std::wstring keyPrefix;         // Optional prefix prepended to every S3 key
        bool zipMultiple{ true };       // Several files shared at once go into one zip (folders are always zipped)
        std::wstring storage{ L"s3" };  // where files go: "s3" (own storage) or "gateway" (a Fling Gateway)
        std::wstring gatewayUrl;        // e.g. https://fling.example.com; the sign-in token is in Credential Manager

        bool UsesGateway() const { return storage == L"gateway"; }

        bool IsS3Configured() const
        {
            return !endpoint.empty() &&
                   !bucket.empty() &&
                   !accessKey.empty() &&
                   !secretKey.empty();
        }

        // Complete enough to attempt uploads (for a gateway, signing in is checked separately).
        bool IsConfigured() const
        {
            return UsesGateway() ? !gatewayUrl.empty() : IsS3Configured();
        }

        // Path-style addressing (https://host/bucket/key) works on every S3-compatible service,
        // while virtual-host style (https://bucket.host/key) needs DNS and TLS for each bucket,
        // which self-hosted servers such as MinIO rarely have. So only Amazon S3 honours the
        // setting; everything else always uses path-style, and a missed checkbox can't break it.
        bool UsesPathStyle() const
        {
            return usePathStyle || !IsAmazonEndpoint();
        }

        bool IsAmazonEndpoint() const
        {
            std::wstring host = endpoint;
            if (auto scheme = host.find(L"://"); scheme != std::wstring::npos)
            {
                host = host.substr(scheme + 3);
            }
            host = host.substr(0, host.find_first_of(L":/"));
            for (auto& c : host)
            {
                c = static_cast<wchar_t>(towlower(c));
            }
            constexpr std::wstring_view aws = L"amazonaws.com";
            return host.empty() || host == aws ||
                (host.size() > aws.size() && host.ends_with(aws) && host[host.size() - aws.size() - 1] == L'.');
        }

        // JSON serialization
        static FlingConfig FromJson(nlohmann::json const& j);
        nlohmann::json ToJson() const;

        // Default configuration
        static FlingConfig Default();
    };

    // Helper functions for wstring <-> string conversion
    inline std::string WideToUtf8(std::wstring const& wide)
    {
        if (wide.empty()) return {};
        int size = WideCharToMultiByte(CP_UTF8, 0, wide.c_str(), static_cast<int>(wide.size()), nullptr, 0, nullptr, nullptr);
        std::string result(size, '\0');
        WideCharToMultiByte(CP_UTF8, 0, wide.c_str(), static_cast<int>(wide.size()), result.data(), size, nullptr, nullptr);
        return result;
    }

    inline std::wstring Utf8ToWide(std::string const& utf8)
    {
        if (utf8.empty()) return {};
        int size = MultiByteToWideChar(CP_UTF8, 0, utf8.c_str(), static_cast<int>(utf8.size()), nullptr, 0);
        std::wstring result(size, L'\0');
        MultiByteToWideChar(CP_UTF8, 0, utf8.c_str(), static_cast<int>(utf8.size()), result.data(), size);
        return result;
    }

    inline FlingConfig FlingConfig::FromJson(nlohmann::json const& j)
    {
        FlingConfig config;

        if (j.contains("endpoint") && j["endpoint"].is_string())
            config.endpoint = Utf8ToWide(j["endpoint"].get<std::string>());

        if (j.contains("region") && j["region"].is_string())
            config.region = Utf8ToWide(j["region"].get<std::string>());

        if (j.contains("bucket") && j["bucket"].is_string())
            config.bucket = Utf8ToWide(j["bucket"].get<std::string>());

        if (j.contains("accessKey") && j["accessKey"].is_string())
            config.accessKey = Utf8ToWide(j["accessKey"].get<std::string>());

        if (j.contains("secretKey") && j["secretKey"].is_string())
            config.secretKey = Utf8ToWide(j["secretKey"].get<std::string>());

        if (j.contains("expirySeconds") && j["expirySeconds"].is_number_integer())
            config.expirySeconds = j["expirySeconds"].get<int>();

        if (j.contains("usePathStyle") && j["usePathStyle"].is_boolean())
            config.usePathStyle = j["usePathStyle"].get<bool>();

        if (j.contains("keyPrefix") && j["keyPrefix"].is_string())
            config.keyPrefix = Utf8ToWide(j["keyPrefix"].get<std::string>());

        if (j.contains("zipMultipleFiles") && j["zipMultipleFiles"].is_boolean())
            config.zipMultiple = j["zipMultipleFiles"].get<bool>();

        if (j.contains("storage") && j["storage"].is_string() && j["storage"].get<std::string>() == "gateway")
            config.storage = L"gateway";

        if (j.contains("gatewayUrl") && j["gatewayUrl"].is_string())
            config.gatewayUrl = Utf8ToWide(j["gatewayUrl"].get<std::string>());

        return config;
    }

    inline nlohmann::json FlingConfig::ToJson() const
    {
        return nlohmann::json{
            {"endpoint", WideToUtf8(endpoint)},
            {"region", WideToUtf8(region)},
            {"bucket", WideToUtf8(bucket)},
            {"accessKey", WideToUtf8(accessKey)},
            {"secretKey", WideToUtf8(secretKey)},
            {"expirySeconds", expirySeconds},
            {"usePathStyle", usePathStyle},
            {"keyPrefix", WideToUtf8(keyPrefix)},
            {"zipMultipleFiles", zipMultiple},
            {"storage", WideToUtf8(storage)},
            {"gatewayUrl", WideToUtf8(gatewayUrl)}
        };
    }

    inline FlingConfig FlingConfig::Default()
    {
        FlingConfig config;
        config.endpoint = L"https://s3.amazonaws.com";
        config.region = L"us-east-1";
        config.expirySeconds = 604800;  // 7 days
        config.usePathStyle = false;
        return config;
    }
}
