#pragma once
#include <atomic>
#include <cstdint>
#include <filesystem>
#include <functional>
#include <optional>
#include <string>

namespace Fling::Core
{
    // Talks to a Fling Gateway: the organisation's server that holds the storage key, decides
    // the rules and hands out short links. Files still go straight to storage, through
    // presigned URLs the gateway signs.
    class GatewayClient
    {
    public:
        GatewayClient(std::wstring baseUrl, std::wstring token);

        struct UploadResult
        {
            bool success{ false };
            std::wstring url;            // the short link
            std::wstring uploadId;
            int64_t expiresInSeconds{ 0 };
            std::wstring errorMessage;
            bool signInNeeded{ false };  // the token was refused: sign in again
        };

        // Announces the upload, PUTs the file to storage, and completes it into a link.
        UploadResult Upload(std::filesystem::path const& file, int64_t ttlSeconds,
                            std::function<void(uint64_t, uint64_t)> const& progress = nullptr,
                            std::atomic<bool> const* cancel = nullptr) const;

        struct Account
        {
            bool success{ false };
            std::wstring email;
            std::wstring errorMessage;
            bool signInNeeded{ false };
        };
        Account Me() const;

        // Ends this computer's session on the gateway. Best effort.
        void SignOut() const;

        struct Token
        {
            bool success{ false };
            std::wstring token;
            std::wstring email;
            std::wstring errorMessage;
        };
        // Exchanges a desktop sign-in code for a token (PKCE).
        static Token ExchangeCode(std::wstring const& baseUrl, std::string const& code, std::string const& verifier,
                                  std::string const& redirectUri);

        // "fling.example.com/" -> "https://fling.example.com". Empty when it isn't usable.
        // http:// is only kept for localhost, for development.
        static std::wstring NormalizeUrl(std::wstring const& input);

    private:
        std::wstring m_base;
        std::wstring m_token;
    };

    // The gateway token lives in Windows Credential Manager, not in config.json.
    namespace GatewayCredentials
    {
        struct Stored
        {
            std::wstring token;
            std::wstring email;
        };
        bool Save(std::wstring const& gatewayUrl, std::wstring const& email, std::wstring const& token);
        std::optional<Stored> Load(std::wstring const& gatewayUrl);
        void Remove(std::wstring const& gatewayUrl);
    }
}
