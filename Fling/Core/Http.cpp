#include "pch.h"
#include "Http.h"

#include <windows.h>
#include <winhttp.h>

#include <cctype>
#include <fstream>
#include <memory>

namespace Fling::Core::Http
{
    namespace
    {
        struct HandleCloser
        {
            void operator()(HINTERNET h) const { if (h) WinHttpCloseHandle(h); }
        };
        using Handle = std::unique_ptr<void, HandleCloser>;

        std::wstring LastError(wchar_t const* what)
        {
            DWORD code = GetLastError();
            switch (code)
            {
            case ERROR_WINHTTP_NAME_NOT_RESOLVED: return L"The server's name couldn't be found. Check the address and your connection.";
            case ERROR_WINHTTP_CANNOT_CONNECT: return L"Couldn't connect to the server.";
            case ERROR_WINHTTP_TIMEOUT: return L"The server didn't answer in time.";
            case ERROR_WINHTTP_SECURE_FAILURE: return L"The server's certificate isn't trusted.";
            case ERROR_WINHTTP_CONNECTION_ERROR: return L"The connection was interrupted.";
            }
            return std::wstring(what) + L" failed (WinHTTP error " + std::to_wstring(code) + L").";
        }

        struct Target
        {
            std::wstring host;
            INTERNET_PORT port{};
            std::wstring pathAndQuery;
            bool secure{};
        };

        bool Crack(std::wstring const& url, Target& t)
        {
            URL_COMPONENTS parts{ sizeof(parts) };
            parts.dwHostNameLength = static_cast<DWORD>(-1);
            parts.dwUrlPathLength = static_cast<DWORD>(-1);
            parts.dwExtraInfoLength = static_cast<DWORD>(-1);
            if (!WinHttpCrackUrl(url.c_str(), static_cast<DWORD>(url.size()), 0, &parts))
            {
                return false;
            }
            t.host.assign(parts.lpszHostName, parts.dwHostNameLength);
            t.port = parts.nPort;
            t.pathAndQuery.assign(parts.lpszUrlPath, parts.dwUrlPathLength);
            t.pathAndQuery.append(parts.lpszExtraInfo, parts.dwExtraInfoLength);
            if (t.pathAndQuery.empty())
            {
                t.pathAndQuery = L"/";
            }
            t.secure = parts.nScheme == INTERNET_SCHEME_HTTPS;
            return !t.host.empty();
        }

        HINTERNET Session()
        {
            // One session for the process; WinHTTP sessions are thread-safe.
            static Handle session = []
            {
                HINTERNET h = WinHttpOpen(L"Fling", WINHTTP_ACCESS_TYPE_AUTOMATIC_PROXY,
                                          WINHTTP_NO_PROXY_NAME, WINHTTP_NO_PROXY_BYPASS, 0);
                if (h)
                {
                    WinHttpSetTimeouts(h, 10000, 10000, 60000, 60000);
                    DWORD protocols = WINHTTP_FLAG_SECURE_PROTOCOL_TLS1_2 | WINHTTP_FLAG_SECURE_PROTOCOL_TLS1_3;
                    WinHttpSetOption(h, WINHTTP_OPTION_SECURE_PROTOCOLS, &protocols, sizeof(protocols));
                }
                return Handle(h);
            }();
            return session.get();
        }

        // Opens a request. WinHTTP doesn't follow redirects here: a 3xx is returned as is.
        Handle Open(std::wstring const& method, std::wstring const& url, Handle& connection, Response& out)
        {
            Target t;
            if (!Crack(url, t))
            {
                out.error = L"That isn't a valid web address.";
                return nullptr;
            }
            if (!Session())
            {
                out.error = LastError(L"Starting WinHTTP");
                return nullptr;
            }
            connection.reset(WinHttpConnect(Session(), t.host.c_str(), t.port, 0));
            if (!connection)
            {
                out.error = LastError(L"Connecting");
                return nullptr;
            }
            Handle request(WinHttpOpenRequest(connection.get(), method.c_str(), t.pathAndQuery.c_str(), nullptr,
                                              WINHTTP_NO_REFERER, WINHTTP_DEFAULT_ACCEPT_TYPES,
                                              t.secure ? WINHTTP_FLAG_SECURE : 0));
            if (!request)
            {
                out.error = LastError(L"Opening the request");
                return nullptr;
            }
            DWORD noRedirects = WINHTTP_DISABLE_REDIRECTS;
            WinHttpSetOption(request.get(), WINHTTP_OPTION_DISABLE_FEATURE, &noRedirects, sizeof(noRedirects));
            return request;
        }

        std::wstring HeaderBlock(Headers const& headers)
        {
            std::wstring block;
            for (auto const& [name, value] : headers)
            {
                block += name + L": " + value + L"\r\n";
            }
            return block;
        }

        void Finish(HINTERNET request, Response& out)
        {
            if (!WinHttpReceiveResponse(request, nullptr))
            {
                out.error = LastError(L"Receiving the response");
                return;
            }
            DWORD status = 0, size = sizeof(status);
            WinHttpQueryHeaders(request, WINHTTP_QUERY_STATUS_CODE | WINHTTP_QUERY_FLAG_NUMBER,
                                WINHTTP_HEADER_NAME_BY_INDEX, &status, &size, WINHTTP_NO_HEADER_INDEX);
            constexpr size_t kMaxBody = 1 << 20;
            DWORD available = 0;
            while (WinHttpQueryDataAvailable(request, &available) && available > 0 && out.body.size() < kMaxBody)
            {
                std::string chunk(available, '\0');
                DWORD read = 0;
                if (!WinHttpReadData(request, chunk.data(), available, &read) || read == 0)
                {
                    break;
                }
                out.body.append(chunk.data(), read);
            }
            out.status = status;
        }
    }

    Response Send(std::wstring const& method, std::wstring const& url, Headers const& headers, std::string const& body)
    {
        Response out;
        Handle connection;
        Handle request = Open(method, url, connection, out);
        if (!request)
        {
            return out;
        }
        auto block = HeaderBlock(headers);
        if (!WinHttpSendRequest(request.get(), block.empty() ? WINHTTP_NO_ADDITIONAL_HEADERS : block.c_str(),
                                block.empty() ? 0 : static_cast<DWORD>(-1),
                                body.empty() ? WINHTTP_NO_REQUEST_DATA : const_cast<char*>(body.data()),
                                static_cast<DWORD>(body.size()), static_cast<DWORD>(body.size()), 0))
        {
            out.error = LastError(L"Sending the request");
            return out;
        }
        Finish(request.get(), out);
        return out;
    }

    Response PutFile(std::wstring const& url, Headers const& headers, std::filesystem::path const& file,
                     std::function<void(uint64_t, uint64_t)> const& progress, std::atomic<bool> const* cancel)
    {
        Response out;
        std::error_code ec;
        uint64_t total = std::filesystem::file_size(file, ec);
        if (ec)
        {
            out.error = L"Couldn't read the file's size.";
            return out;
        }
        if (total >= 0xFFFFFFFFull)
        {
            out.error = L"Files of 4 GB or more can't be uploaded through a gateway yet.";
            return out;
        }
        std::ifstream in(file, std::ios::binary);
        if (!in)
        {
            out.error = L"Couldn't open the file for reading.";
            return out;
        }

        Handle connection;
        Handle request = Open(L"PUT", url, connection, out);
        if (!request)
        {
            return out;
        }
        // WinHTTP adds Content-Length from the total, so a Content-Length here would be sent twice.
        Headers rest;
        for (auto const& h : headers)
        {
            if (_wcsicmp(h.first.c_str(), L"Content-Length") != 0)
            {
                rest.push_back(h);
            }
        }
        auto block = HeaderBlock(rest);
        if (!WinHttpSendRequest(request.get(), block.empty() ? WINHTTP_NO_ADDITIONAL_HEADERS : block.c_str(),
                                block.empty() ? 0 : static_cast<DWORD>(-1), WINHTTP_NO_REQUEST_DATA, 0,
                                static_cast<DWORD>(total), 0))
        {
            out.error = LastError(L"Starting the upload");
            return out;
        }

        std::vector<char> buffer(1 << 20);
        uint64_t sent = 0;
        while (sent < total)
        {
            if (cancel && cancel->load())
            {
                out.error = L"Upload cancelled";
                return out;
            }
            in.read(buffer.data(), static_cast<std::streamsize>(std::min<uint64_t>(buffer.size(), total - sent)));
            auto got = static_cast<DWORD>(in.gcount());
            if (got == 0)
            {
                out.error = L"The file changed while it was being uploaded.";
                return out;
            }
            DWORD written = 0;
            if (!WinHttpWriteData(request.get(), buffer.data(), got, &written) || written != got)
            {
                out.error = LastError(L"Uploading");
                return out;
            }
            sent += got;
            if (progress)
            {
                progress(sent, total);
            }
        }
        Finish(request.get(), out);
        return out;
    }

    std::string Encode(std::string const& text)
    {
        static constexpr char kHex[] = "0123456789ABCDEF";
        std::string out;
        for (unsigned char c : text)
        {
            if (std::isalnum(c) || c == '-' || c == '.' || c == '_' || c == '~')
            {
                out.push_back(static_cast<char>(c));
            }
            else
            {
                out.push_back('%');
                out.push_back(kHex[c >> 4]);
                out.push_back(kHex[c & 15]);
            }
        }
        return out;
    }

    std::string Decode(std::string const& text)
    {
        auto hex = [](char c) -> int {
            if (c >= '0' && c <= '9') return c - '0';
            if (c >= 'a' && c <= 'f') return c - 'a' + 10;
            if (c >= 'A' && c <= 'F') return c - 'A' + 10;
            return -1;
        };
        std::string out;
        for (size_t i = 0; i < text.size(); ++i)
        {
            if (text[i] == '+')
            {
                out.push_back(' ');
            }
            else if (text[i] == '%' && i + 2 < text.size() && hex(text[i + 1]) >= 0 && hex(text[i + 2]) >= 0)
            {
                out.push_back(static_cast<char>(hex(text[i + 1]) * 16 + hex(text[i + 2])));
                i += 2;
            }
            else
            {
                out.push_back(text[i]);
            }
        }
        return out;
    }

    std::map<std::string, std::string> ParseQuery(std::string const& query)
    {
        std::map<std::string, std::string> out;
        size_t start = 0;
        while (start <= query.size())
        {
            size_t end = query.find('&', start);
            if (end == std::string::npos)
            {
                end = query.size();
            }
            auto pair = query.substr(start, end - start);
            if (!pair.empty())
            {
                auto eq = pair.find('=');
                auto key = Decode(pair.substr(0, eq));
                out.try_emplace(key, eq == std::string::npos ? std::string() : Decode(pair.substr(eq + 1)));
            }
            start = end + 1;
        }
        return out;
    }

    std::string FormEncode(std::map<std::string, std::string> const& fields)
    {
        std::string out;
        for (auto const& [key, value] : fields)
        {
            if (!out.empty())
            {
                out.push_back('&');
            }
            out += Encode(key) + "=" + Encode(value);
        }
        return out;
    }
}
