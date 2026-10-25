#include "pch.h"
#include "GatewaySignIn.h"

#include "GatewayClient.h"
#include "Http.h"
#include "Log.h"
#include "../Models/FlingConfig.h"

#include <winsock2.h>
#include <ws2tcpip.h>
#include <windows.h>
#include <bcrypt.h>
#include <shellapi.h>

#include <vector>

using Fling::Models::Utf8ToWide;
using Fling::Models::WideToUtf8;

namespace Fling::Core::GatewaySignIn
{
    namespace
    {
        std::string Base64Url(std::vector<unsigned char> const& bytes)
        {
            static constexpr char kAlphabet[] = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_";
            std::string out;
            size_t i = 0;
            for (; i + 2 < bytes.size(); i += 3)
            {
                uint32_t n = (bytes[i] << 16) | (bytes[i + 1] << 8) | bytes[i + 2];
                out += kAlphabet[(n >> 18) & 63];
                out += kAlphabet[(n >> 12) & 63];
                out += kAlphabet[(n >> 6) & 63];
                out += kAlphabet[n & 63];
            }
            if (i + 1 == bytes.size())
            {
                uint32_t n = bytes[i] << 16;
                out += kAlphabet[(n >> 18) & 63];
                out += kAlphabet[(n >> 12) & 63];
            }
            else if (i + 2 == bytes.size())
            {
                uint32_t n = (bytes[i] << 16) | (bytes[i + 1] << 8);
                out += kAlphabet[(n >> 18) & 63];
                out += kAlphabet[(n >> 12) & 63];
                out += kAlphabet[(n >> 6) & 63];
            }
            return out;
        }

        struct WinsockInit
        {
            bool ok;
            WinsockInit() { WSADATA d; ok = WSAStartup(MAKEWORD(2, 2), &d) == 0; }
            ~WinsockInit() { if (ok) WSACleanup(); }
        };

        void Reply(SOCKET client, int status, std::string const& title, std::string const& message)
        {
            std::string page =
                "<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\"><title>" + title + " \xC2\xB7 Fling</title>"
                "<style>:root{color-scheme:light dark}body{margin:0;min-height:100vh;display:grid;place-items:center;"
                "font:14px/20px 'Segoe UI Variable Text','Segoe UI',system-ui,sans-serif;background:#FAF8F5;color:#1E1C1A}"
                "@media (prefers-color-scheme:dark){body{background:#171615;color:#F2EEE8}}"
                "main{max-width:420px;padding:24px}h1{font-size:20px;margin:0 0 4px}p{margin:0;opacity:.75}</style></head>"
                "<body><main><h1>" + title + "</h1><p>" + message + "</p></main></body></html>";
            std::string response = "HTTP/1.1 " + std::to_string(status) + (status == 200 ? " OK" : " Not Found") +
                "\r\nContent-Type: text/html; charset=utf-8\r\nCache-Control: no-store\r\nConnection: close\r\n"
                "Content-Length: " + std::to_string(page.size()) + "\r\n\r\n" + page;
            send(client, response.data(), static_cast<int>(response.size()), 0);
        }
    }

    std::string RandomToken(size_t bytes)
    {
        std::vector<unsigned char> buffer(bytes);
        BCryptGenRandom(nullptr, buffer.data(), static_cast<ULONG>(buffer.size()), BCRYPT_USE_SYSTEM_PREFERRED_RNG);
        return Base64Url(buffer);
    }

    std::string NewVerifier() { return RandomToken(32); }

    std::string Challenge(std::string const& verifier)
    {
        std::vector<unsigned char> digest(32);
        BCryptHash(BCRYPT_SHA256_ALG_HANDLE, nullptr, 0,
                   reinterpret_cast<PUCHAR>(const_cast<char*>(verifier.data())), static_cast<ULONG>(verifier.size()),
                   digest.data(), static_cast<ULONG>(digest.size()));
        return Base64Url(digest);
    }

    LoopbackListener::LoopbackListener()
    {
        static WinsockInit winsock;
        if (!winsock.ok)
        {
            return;
        }
        SOCKET s = socket(AF_INET, SOCK_STREAM, IPPROTO_TCP);
        if (s == INVALID_SOCKET)
        {
            return;
        }
        sockaddr_in addr{};
        addr.sin_family = AF_INET;
        addr.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
        addr.sin_port = 0; // any free port
        int len = sizeof(addr);
        if (bind(s, reinterpret_cast<sockaddr*>(&addr), sizeof(addr)) != 0 || listen(s, 4) != 0 ||
            getsockname(s, reinterpret_cast<sockaddr*>(&addr), &len) != 0)
        {
            closesocket(s);
            return;
        }
        m_socket = static_cast<uint64_t>(s);
        m_port = ntohs(addr.sin_port);
    }

    LoopbackListener::~LoopbackListener()
    {
        if (Ok())
        {
            closesocket(static_cast<SOCKET>(m_socket));
        }
    }

    std::map<std::string, std::string> LoopbackListener::WaitForCallback(std::atomic<bool> const& cancel,
                                                                        std::chrono::steady_clock::time_point deadline)
    {
        auto listener = static_cast<SOCKET>(m_socket);
        while (Ok() && !cancel.load() && std::chrono::steady_clock::now() < deadline)
        {
            fd_set ready;
            FD_ZERO(&ready);
            FD_SET(listener, &ready);
            timeval wait{ 0, 250 * 1000 };
            if (select(0, &ready, nullptr, nullptr, &wait) <= 0)
            {
                continue;
            }
            SOCKET client = accept(listener, nullptr, nullptr);
            if (client == INVALID_SOCKET)
            {
                continue;
            }
            DWORD timeoutMs = 5000;
            setsockopt(client, SOL_SOCKET, SO_RCVTIMEO, reinterpret_cast<char const*>(&timeoutMs), sizeof(timeoutMs));
            std::string request;
            char buffer[2048];
            while (request.find("\r\n") == std::string::npos && request.size() < 16384)
            {
                int got = recv(client, buffer, sizeof(buffer), 0);
                if (got <= 0)
                {
                    break;
                }
                request.append(buffer, got);
            }
            // "GET /callback?code=...&state=... HTTP/1.1"
            auto lineEnd = request.find("\r\n");
            auto line = request.substr(0, lineEnd == std::string::npos ? request.size() : lineEnd);
            std::string target;
            if (line.rfind("GET ", 0) == 0)
            {
                target = line.substr(4, line.find(' ', 4) - 4);
            }
            if (target.rfind("/callback?", 0) != 0)
            {
                Reply(client, 404, "Not found", "This address is only for signing in to Fling.");
                closesocket(client);
                continue;
            }
            auto params = Http::ParseQuery(target.substr(10));
            if (params.count("code"))
            {
                Reply(client, 200, "You're signed in", "Go back to Fling. You can close this tab.");
            }
            else
            {
                Reply(client, 200, "Sign-in didn't finish", "Go back to Fling and try again.");
            }
            closesocket(client);
            return params;
        }
        return {};
    }

    Result SignIn(std::wstring const& gatewayUrl, std::atomic<bool> const& cancel, std::chrono::seconds timeout)
    {
        Result out;
        LoopbackListener listener;
        if (!listener.Ok())
        {
            out.errorMessage = L"Fling couldn't open a local port to finish the sign-in.";
            return out;
        }
        auto verifier = NewVerifier();
        auto state = RandomToken(16);
        auto redirect = "http://127.0.0.1:" + std::to_string(listener.Port()) + "/callback";
        auto start = gatewayUrl + L"/auth/desktop?" + Utf8ToWide(Http::FormEncode({
            { "redirect_uri", redirect },
            { "state", state },
            { "code_challenge", Challenge(verifier) },
            { "code_challenge_method", "S256" },
        }));
        Log::Info(L"Gateway sign-in: opening the browser");
        if (reinterpret_cast<INT_PTR>(ShellExecuteW(nullptr, L"open", start.c_str(), nullptr, nullptr, SW_SHOWNORMAL)) <= 32)
        {
            out.errorMessage = L"Fling couldn't open your browser.";
            return out;
        }

        auto params = listener.WaitForCallback(cancel, std::chrono::steady_clock::now() + timeout);
        if (cancel.load())
        {
            out.errorMessage = L"Sign-in cancelled.";
            return out;
        }
        if (params.empty())
        {
            out.errorMessage = L"Sign-in timed out. Try again.";
            return out;
        }
        if (params["state"] != state || params["code"].empty())
        {
            Log::Warning(L"Gateway sign-in: the browser came back without a valid code");
            out.errorMessage = L"Sign-in didn't finish. Try again.";
            return out;
        }
        auto token = GatewayClient::ExchangeCode(gatewayUrl, params["code"], verifier, redirect);
        if (!token.success)
        {
            Log::Warning(L"Gateway sign-in: token exchange failed: " + token.errorMessage);
            out.errorMessage = token.errorMessage;
            return out;
        }
        Log::Info(L"Gateway sign-in finished");
        out.success = true;
        out.token = token.token;
        out.email = token.email;
        return out;
    }
}
