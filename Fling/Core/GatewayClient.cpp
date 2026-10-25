#include "pch.h"
#include "GatewayClient.h"

#include "ContentType.h"
#include "Http.h"
#include "../Models/FlingConfig.h"

#include <windows.h>
#include <wincred.h>

#include <algorithm>
#include <ctime>
#include <nlohmann/json.hpp>

using Fling::Models::Utf8ToWide;
using Fling::Models::WideToUtf8;
using nlohmann::json;

namespace Fling::Core
{
    namespace
    {
        // The gateway's errors look like {"error": {"code": "...", "message": "..."}}.
        std::wstring GatewayMessage(Http::Response const& r, std::wstring const& fallback)
        {
            if (r.status == 0)
            {
                return r.error.empty() ? fallback : r.error;
            }
            auto j = json::parse(r.body, nullptr, false);
            if (j.is_object() && j.contains("error") && j["error"].is_object() && j["error"].contains("message") &&
                j["error"]["message"].is_string())
            {
                return Utf8ToWide(j["error"]["message"].get<std::string>());
            }
            return fallback + L" (HTTP " + std::to_wstring(r.status) + L")";
        }

        std::string JsonString(json const& j, char const* key)
        {
            return j.contains(key) && j[key].is_string() ? j[key].get<std::string>() : std::string();
        }

        // "2026-11-01T19:31:00Z" (or with fractional seconds) -> Unix time; 0 if unreadable.
        int64_t ParseRfc3339(std::string const& text)
        {
            std::tm tm{};
            if (sscanf_s(text.c_str(), "%d-%d-%dT%d:%d:%d", &tm.tm_year, &tm.tm_mon, &tm.tm_mday, &tm.tm_hour,
                         &tm.tm_min, &tm.tm_sec) != 6)
            {
                return 0;
            }
            tm.tm_year -= 1900;
            tm.tm_mon -= 1;
            return static_cast<int64_t>(_mkgmtime(&tm));
        }

        std::wstring CredentialTarget(std::wstring const& gatewayUrl)
        {
            return L"Fling Gateway " + gatewayUrl;
        }
    }

    GatewayClient::GatewayClient(std::wstring baseUrl, std::wstring token)
        : m_base(std::move(baseUrl)), m_token(std::move(token))
    {
    }

    std::wstring GatewayClient::NormalizeUrl(std::wstring const& input)
    {
        std::wstring url = input;
        url.erase(0, url.find_first_not_of(L" \t"));
        url.erase(url.find_last_not_of(L" \t/") + 1);
        if (url.empty())
        {
            return {};
        }
        std::wstring lower = url;
        std::transform(lower.begin(), lower.end(), lower.begin(), ::towlower);
        if (lower.rfind(L"https://", 0) == 0)
        {
            return url;
        }
        if (lower.rfind(L"http://", 0) == 0)
        {
            auto host = lower.substr(7, lower.find_first_of(L":/", 7) - 7);
            return (host == L"localhost" || host == L"127.0.0.1") ? url : std::wstring();
        }
        if (lower.find(L"://") != std::wstring::npos)
        {
            return {};
        }
        return L"https://" + url;
    }

    GatewayClient::UploadResult GatewayClient::Upload(std::filesystem::path const& file, int64_t ttlSeconds,
                                                      std::function<void(uint64_t, uint64_t)> const& progress,
                                                      std::atomic<bool> const* cancel) const
    {
        UploadResult out;
        std::error_code ec;
        auto size = std::filesystem::file_size(file, ec);
        if (ec)
        {
            out.errorMessage = L"Couldn't read the file's size.";
            return out;
        }
        Http::Headers auth = { { L"Authorization", L"Bearer " + m_token } };
        Http::Headers jsonAuth = auth;
        jsonAuth.emplace_back(L"Content-Type", L"application/json");

        // 1. Announce the upload; the gateway checks its rules and signs a PUT.
        json request = {
            { "name", WideToUtf8(file.filename().wstring()) },
            { "size", size },
            { "contentType", ContentTypeFor(file) },
            { "ttlSeconds", ttlSeconds },
        };
        auto created = Http::Send(L"POST", m_base + L"/api/v1/uploads", jsonAuth, request.dump());
        if (created.status == 401)
        {
            out.signInNeeded = true;
            out.errorMessage = L"Your gateway sign-in has ended. Sign in again in Settings.";
            return out;
        }
        if (created.status != 201)
        {
            out.errorMessage = GatewayMessage(created, L"The gateway didn't accept the upload.");
            return out;
        }
        auto target = json::parse(created.body, nullptr, false);
        if (!target.is_object() || !target.contains("upload") || !target["upload"].is_object())
        {
            out.errorMessage = L"The gateway sent an answer Fling doesn't understand.";
            return out;
        }
        out.uploadId = Utf8ToWide(JsonString(target, "uploadId"));
        auto const& upload = target["upload"];
        Http::Headers putHeaders;
        if (upload.contains("headers") && upload["headers"].is_object())
        {
            for (auto const& [name, value] : upload["headers"].items())
            {
                if (value.is_string())
                {
                    putHeaders.emplace_back(Utf8ToWide(name), Utf8ToWide(value.get<std::string>()));
                }
            }
        }

        // 2. Send the file straight to storage.
        auto put = Http::PutFile(Utf8ToWide(JsonString(upload, "url")), putHeaders, file, progress, cancel);
        if (!put.Ok())
        {
            out.errorMessage = put.status == 0 ? put.error
                : L"The storage refused the upload (HTTP " + std::to_wstring(put.status) + L").";
            return out;
        }

        // 3. Turn it into a link.
        auto done = Http::Send(L"POST", m_base + L"/api/v1/uploads/" + out.uploadId + L"/complete", auth);
        if (done.status == 401)
        {
            out.signInNeeded = true;
            out.errorMessage = L"Your gateway sign-in has ended. Sign in again in Settings.";
            return out;
        }
        if (done.status != 200)
        {
            out.errorMessage = GatewayMessage(done, L"The gateway couldn't make the link.");
            return out;
        }
        auto link = json::parse(done.body, nullptr, false);
        out.url = Utf8ToWide(JsonString(link, "url"));
        if (out.url.empty())
        {
            out.errorMessage = L"The gateway sent an answer Fling doesn't understand.";
            return out;
        }
        auto expires = ParseRfc3339(JsonString(link, "expiresAt"));
        out.expiresInSeconds = expires > 0 ? std::max<int64_t>(0, expires - static_cast<int64_t>(std::time(nullptr))) : ttlSeconds;
        out.success = true;
        return out;
    }

    GatewayClient::Account GatewayClient::Me() const
    {
        Account out;
        auto r = Http::Send(L"GET", m_base + L"/api/v1/me", { { L"Authorization", L"Bearer " + m_token } });
        if (r.status == 401)
        {
            out.signInNeeded = true;
            out.errorMessage = L"Not signed in.";
            return out;
        }
        if (!r.Ok())
        {
            out.errorMessage = GatewayMessage(r, L"The gateway didn't answer as expected.");
            return out;
        }
        auto j = json::parse(r.body, nullptr, false);
        out.email = Utf8ToWide(JsonString(j, "email"));
        out.success = !out.email.empty();
        if (!out.success)
        {
            out.errorMessage = L"That address doesn't look like a Fling Gateway.";
        }
        return out;
    }

    void GatewayClient::SignOut() const
    {
        Http::Send(L"DELETE", m_base + L"/api/v1/session", { { L"Authorization", L"Bearer " + m_token } });
    }

    GatewayClient::Token GatewayClient::ExchangeCode(std::wstring const& baseUrl, std::string const& code,
                                                     std::string const& verifier, std::string const& redirectUri)
    {
        Token out;
        auto body = Http::FormEncode({ { "grant_type", "authorization_code" }, { "code", code },
                                       { "code_verifier", verifier }, { "redirect_uri", redirectUri } });
        auto r = Http::Send(L"POST", baseUrl + L"/api/v1/token",
                            { { L"Content-Type", L"application/x-www-form-urlencoded" } }, body);
        auto j = json::parse(r.body, nullptr, false);
        if (!r.Ok())
        {
            out.errorMessage = r.status == 0 ? r.error
                : (j.is_object() && j.contains("error_description") && j["error_description"].is_string())
                    ? Utf8ToWide(j["error_description"].get<std::string>())
                    : L"The gateway refused the sign-in (HTTP " + std::to_wstring(r.status) + L").";
            return out;
        }
        out.token = Utf8ToWide(JsonString(j, "access_token"));
        out.email = Utf8ToWide(JsonString(j, "email"));
        out.success = !out.token.empty();
        if (!out.success)
        {
            out.errorMessage = L"The gateway sent an answer Fling doesn't understand.";
        }
        return out;
    }

    namespace GatewayCredentials
    {
        bool Save(std::wstring const& gatewayUrl, std::wstring const& email, std::wstring const& token)
        {
            auto target = CredentialTarget(gatewayUrl);
            auto blob = WideToUtf8(token);
            CREDENTIALW cred{};
            cred.Type = CRED_TYPE_GENERIC;
            cred.TargetName = const_cast<wchar_t*>(target.c_str());
            cred.UserName = const_cast<wchar_t*>(email.c_str());
            cred.CredentialBlobSize = static_cast<DWORD>(blob.size());
            cred.CredentialBlob = reinterpret_cast<LPBYTE>(blob.data());
            cred.Persist = CRED_PERSIST_LOCAL_MACHINE;
            return CredWriteW(&cred, 0) != FALSE;
        }

        std::optional<Stored> Load(std::wstring const& gatewayUrl)
        {
            PCREDENTIALW cred = nullptr;
            if (!CredReadW(CredentialTarget(gatewayUrl).c_str(), CRED_TYPE_GENERIC, 0, &cred))
            {
                return std::nullopt;
            }
            Stored out;
            out.token = Utf8ToWide(std::string(reinterpret_cast<char const*>(cred->CredentialBlob), cred->CredentialBlobSize));
            out.email = cred->UserName ? cred->UserName : L"";
            CredFree(cred);
            if (out.token.empty())
            {
                return std::nullopt;
            }
            return out;
        }

        void Remove(std::wstring const& gatewayUrl)
        {
            CredDeleteW(CredentialTarget(gatewayUrl).c_str(), CRED_TYPE_GENERIC, 0);
        }
    }
}
