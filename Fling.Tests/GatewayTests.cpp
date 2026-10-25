#include "pch.h"
#include "Core/GatewayClient.h"
#include "Core/GatewaySignIn.h"
#include "Core/Http.h"
#include "Models/FlingConfig.h"

#include <cstdlib>
#include <thread>

using namespace Fling::Core;

// RFC 7636, appendix B.
TEST(GatewaySignInTests, ChallengeMatchesRfc7636Example)
{
    EXPECT_EQ(GatewaySignIn::Challenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"),
              "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM");
}

TEST(GatewaySignInTests, VerifierIsRandomAndUrlSafe)
{
    auto a = GatewaySignIn::NewVerifier();
    auto b = GatewaySignIn::NewVerifier();
    EXPECT_EQ(a.size(), 43u);  // 32 bytes, base64url without padding
    EXPECT_NE(a, b);
    EXPECT_EQ(a.find_first_not_of("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"), std::string::npos);
}

TEST(HttpTests, EncodeDecodeAndQueries)
{
    EXPECT_EQ(Http::Encode("a b/c?d=é"), "a%20b%2Fc%3Fd%3D%C3%A9");
    EXPECT_EQ(Http::Decode("a+b%2Bc%3d%zz%4"), "a b+c=%zz%4");
    auto q = Http::ParseQuery("code=x%2By&state=s+t&empty=&flag&code=second");
    EXPECT_EQ(q["code"], "x+y");  // the first value wins
    EXPECT_EQ(q["state"], "s t");
    EXPECT_EQ(q["empty"], "");
    EXPECT_TRUE(q.count("flag"));
    EXPECT_EQ(Http::FormEncode({ { "b", "2 3" }, { "a", "x&y" } }), "a=x%26y&b=2%203");
}

TEST(GatewayClientTests, NormalizeUrl)
{
    EXPECT_EQ(GatewayClient::NormalizeUrl(L"  fling.example.com/ "), L"https://fling.example.com");
    EXPECT_EQ(GatewayClient::NormalizeUrl(L"https://fling.example.com"), L"https://fling.example.com");
    EXPECT_EQ(GatewayClient::NormalizeUrl(L"HTTPS://Fling.Example.com//"), L"HTTPS://Fling.Example.com");
    EXPECT_EQ(GatewayClient::NormalizeUrl(L"http://localhost:8080/"), L"http://localhost:8080");
    EXPECT_EQ(GatewayClient::NormalizeUrl(L"http://fling.example.com"), L"");  // no plain http on the internet
    EXPECT_EQ(GatewayClient::NormalizeUrl(L"ftp://fling.example.com"), L"");
    EXPECT_EQ(GatewayClient::NormalizeUrl(L"   "), L"");
}

TEST(GatewaySignInTests, LoopbackListenerReturnsTheCallback)
{
    GatewaySignIn::LoopbackListener listener;
    ASSERT_TRUE(listener.Ok());
    ASSERT_GT(listener.Port(), 0);
    auto base = L"http://127.0.0.1:" + std::to_wstring(listener.Port());

    Http::Response favicon, callback;
    std::thread browser([&] {
        favicon = Http::Send(L"GET", base + L"/favicon.ico", {});
        callback = Http::Send(L"GET", base + L"/callback?code=a%2Bb&state=s%20t", {});
    });
    std::atomic<bool> cancel{ false };
    auto params = listener.WaitForCallback(cancel, std::chrono::steady_clock::now() + std::chrono::seconds(10));
    browser.join();

    EXPECT_EQ(favicon.status, 404u);
    EXPECT_EQ(callback.status, 200u);
    EXPECT_NE(callback.body.find("You're signed in"), std::string::npos);
    EXPECT_EQ(params["code"], "a+b");
    EXPECT_EQ(params["state"], "s t");
}

TEST(GatewaySignInTests, LoopbackListenerStopsOnCancel)
{
    GatewaySignIn::LoopbackListener listener;
    std::atomic<bool> cancel{ false };
    std::thread canceller([&] {
        std::this_thread::sleep_for(std::chrono::milliseconds(300));
        cancel = true;
    });
    auto started = std::chrono::steady_clock::now();
    auto params = listener.WaitForCallback(cancel, started + std::chrono::seconds(30));
    canceller.join();
    EXPECT_TRUE(params.empty());
    EXPECT_LT(std::chrono::steady_clock::now() - started, std::chrono::seconds(5));
}

TEST(GatewayCredentialsTests, SaveLoadRemove)
{
    const std::wstring url = L"https://fling-tests.invalid";
    GatewayCredentials::Remove(url);
    EXPECT_FALSE(GatewayCredentials::Load(url).has_value());

    ASSERT_TRUE(GatewayCredentials::Save(url, L"ann@example.com", L"token-åäö-123"));
    auto stored = GatewayCredentials::Load(url);
    ASSERT_TRUE(stored.has_value());
    EXPECT_EQ(stored->email, L"ann@example.com");
    EXPECT_EQ(stored->token, L"token-åäö-123");
    EXPECT_FALSE(GatewayCredentials::Load(url + L"/other").has_value());

    GatewayCredentials::Remove(url);
    EXPECT_FALSE(GatewayCredentials::Load(url).has_value());
}

// Against the local dev gateway (gateway/compose.dev.yml), which accepts a development token.
// Skipped when it isn't running.
class GatewayIntegrationTests : public ::testing::Test
{
protected:
    std::wstring base = L"http://localhost:8080";
    std::wstring token = L"dev-token-change-me-0123456789abcdef";

    void SetUp() override
    {
        if (GatewayClient(base, token).Me().success == false &&
            Http::Send(L"GET", base + L"/healthz", {}).status != 200)
        {
            GTEST_SKIP() << "dev gateway not running (docker compose -f gateway/compose.dev.yml up)";
        }
    }
};

TEST_F(GatewayIntegrationTests, MeAndRefusedToken)
{
    auto me = GatewayClient(base, token).Me();
    ASSERT_TRUE(me.success) << Fling::Models::WideToUtf8(me.errorMessage);
    EXPECT_EQ(me.email, L"dev@localhost");
    auto refused = GatewayClient(base, L"not-a-token").Me();
    EXPECT_FALSE(refused.success);
    EXPECT_TRUE(refused.signInNeeded);
}

TEST_F(GatewayIntegrationTests, UploadMakesAWorkingLink)
{
    auto file = std::filesystem::temp_directory_path() / L"fling gateway test å.txt";
    {
        std::ofstream out(file, std::ios::binary);
        for (int i = 0; i < 300000; ++i) out << "fling ";
    }
    uint64_t lastSent = 0, lastTotal = 0;
    int calls = 0;
    auto result = GatewayClient(base, token).Upload(file, 3600, [&](uint64_t sent, uint64_t total) {
        EXPECT_GE(sent, lastSent);
        lastSent = sent;
        lastTotal = total;
        ++calls;
    });
    ASSERT_TRUE(result.success) << Fling::Models::WideToUtf8(result.errorMessage);
    EXPECT_EQ(result.url.rfind(base + L"/l/", 0), 0u);
    EXPECT_GT(result.expiresInSeconds, 3000);
    EXPECT_LE(result.expiresInSeconds, 3600);
    EXPECT_EQ(lastTotal, std::filesystem::file_size(file));
    EXPECT_EQ(lastSent, lastTotal);
    EXPECT_GE(calls, 2);  // 1.8 MB in 1 MiB chunks

    // The link redirects to a presigned download of the same bytes.
    auto link = Http::Send(L"GET", result.url, {});
    EXPECT_EQ(link.status, 302u);

    std::filesystem::remove(file);
}

TEST_F(GatewayIntegrationTests, UploadCanBeCancelled)
{
    auto file = std::filesystem::temp_directory_path() / L"fling gateway cancel.bin";
    {
        std::ofstream out(file, std::ios::binary);
        std::string mb(1 << 20, 'x');
        for (int i = 0; i < 5; ++i) out << mb;
    }
    std::atomic<bool> cancel{ false };
    auto result = GatewayClient(base, token).Upload(file, 3600, [&](uint64_t sent, uint64_t) {
        if (sent >= (1 << 20)) cancel = true;
    }, &cancel);
    EXPECT_FALSE(result.success);
    EXPECT_EQ(result.errorMessage, L"Upload cancelled");
    std::filesystem::remove(file);
}
