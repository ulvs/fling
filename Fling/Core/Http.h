#pragma once
#include <atomic>
#include <cstdint>
#include <filesystem>
#include <functional>
#include <map>
#include <string>
#include <utility>
#include <vector>

namespace Fling::Core::Http
{
    using Headers = std::vector<std::pair<std::wstring, std::wstring>>;

    struct Response
    {
        unsigned status{ 0 };     // 0: no response (network error, cancelled)
        std::string body;
        std::wstring error;       // set when status is 0

        bool Ok() const { return status >= 200 && status < 300; }
    };

    // A request with a small body (JSON or a form). Follows no redirects.
    Response Send(std::wstring const& method, std::wstring const& url, Headers const& headers,
                  std::string const& body = {});

    // PUTs a file in 1 MiB chunks, reporting (bytesSent, total) after each one. cancel, if
    // given, is checked between chunks. Content-Length is set from the file size.
    Response PutFile(std::wstring const& url, Headers const& headers, std::filesystem::path const& file,
                     std::function<void(uint64_t, uint64_t)> const& progress = nullptr,
                     std::atomic<bool> const* cancel = nullptr);

    // Percent-encoding for query strings and forms (RFC 3986 unreserved characters kept).
    std::string Encode(std::string const& text);
    std::string Decode(std::string const& text);  // also turns '+' into a space

    // "a=1&b=x%20y" -> {a: "1", b: "x y"}
    std::map<std::string, std::string> ParseQuery(std::string const& query);
    std::string FormEncode(std::map<std::string, std::string> const& fields);
}
