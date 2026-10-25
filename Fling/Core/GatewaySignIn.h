#pragma once
#include <atomic>
#include <chrono>
#include <cstdint>
#include <map>
#include <string>

namespace Fling::Core
{
    // Signing in to a Fling Gateway from the desktop (RFC 8252): Fling listens once on a
    // loopback port, opens the browser at the gateway, and the gateway sends the browser back
    // to that port with a one-time code. PKCE (RFC 7636) proves the code reached this app.
    namespace GatewaySignIn
    {
        struct Result
        {
            bool success{ false };
            std::wstring token;
            std::wstring email;
            std::wstring errorMessage;
        };

        // Blocks until the person finishes in the browser, cancel is set, or timeout passes.
        Result SignIn(std::wstring const& gatewayUrl, std::atomic<bool> const& cancel,
                      std::chrono::seconds timeout = std::chrono::minutes(10));

        // PKCE: a random 43-character verifier, and its S256 challenge.
        std::string NewVerifier();
        std::string Challenge(std::string const& verifier);
        std::string RandomToken(size_t bytes);

        // A one-shot HTTP listener on 127.0.0.1 for the browser's redirect.
        class LoopbackListener
        {
        public:
            LoopbackListener();
            ~LoopbackListener();
            LoopbackListener(LoopbackListener const&) = delete;
            LoopbackListener& operator=(LoopbackListener const&) = delete;

            bool Ok() const { return m_socket != ~uint64_t(0); }
            uint16_t Port() const { return m_port; }

            // Waits for GET /callback?... and returns its query parameters (empty on timeout
            // or cancel). Other requests (favicon) get a 404. The browser gets a small page
            // that says whether to go back to Fling.
            std::map<std::string, std::string> WaitForCallback(std::atomic<bool> const& cancel,
                                                               std::chrono::steady_clock::time_point deadline);

        private:
            uint64_t m_socket{ ~uint64_t(0) };
            uint16_t m_port{ 0 };
        };
    }
}
