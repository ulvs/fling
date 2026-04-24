#include "pch.h"
#include "Models/FlingConfig.h"

using namespace Fling::Models;

class FlingConfigTests : public ::testing::Test
{
protected:
    void SetUp() override {}
    void TearDown() override {}
};

TEST_F(FlingConfigTests, DefaultValues)
{
    FlingConfig config = FlingConfig::Default();

    EXPECT_EQ(config.endpoint, L"https://s3.amazonaws.com");
    EXPECT_EQ(config.region, L"us-east-1");
    EXPECT_TRUE(config.bucket.empty());
    EXPECT_TRUE(config.accessKey.empty());
    EXPECT_TRUE(config.secretKey.empty());
    EXPECT_EQ(config.expirySeconds, 604800);
    EXPECT_FALSE(config.usePathStyle);
}

TEST_F(FlingConfigTests, IsConfigured_WithAllFields)
{
    FlingConfig config;
    config.endpoint = L"http://localhost:9000";
    config.bucket = L"test-bucket";
    config.accessKey = L"access-key";
    config.secretKey = L"secret-key";

    EXPECT_TRUE(config.IsConfigured());
}

TEST_F(FlingConfigTests, IsConfigured_MissingEndpoint)
{
    FlingConfig config;
    config.bucket = L"test-bucket";
    config.accessKey = L"access-key";
    config.secretKey = L"secret-key";

    EXPECT_FALSE(config.IsConfigured());
}

TEST_F(FlingConfigTests, IsConfigured_MissingBucket)
{
    FlingConfig config;
    config.endpoint = L"http://localhost:9000";
    config.accessKey = L"access-key";
    config.secretKey = L"secret-key";

    EXPECT_FALSE(config.IsConfigured());
}

TEST_F(FlingConfigTests, IsConfigured_MissingAccessKey)
{
    FlingConfig config;
    config.endpoint = L"http://localhost:9000";
    config.bucket = L"test-bucket";
    config.secretKey = L"secret-key";

    EXPECT_FALSE(config.IsConfigured());
}

TEST_F(FlingConfigTests, IsConfigured_MissingSecretKey)
{
    FlingConfig config;
    config.endpoint = L"http://localhost:9000";
    config.bucket = L"test-bucket";
    config.accessKey = L"access-key";

    EXPECT_FALSE(config.IsConfigured());
}

TEST_F(FlingConfigTests, JsonRoundTrip)
{
    FlingConfig original;
    original.endpoint = L"http://localhost:9000";
    original.region = L"eu-west-1";
    original.bucket = L"my-bucket";
    original.accessKey = L"AKIAIOSFODNN7EXAMPLE";
    original.secretKey = L"wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY";
    original.expirySeconds = 86400;
    original.usePathStyle = true;

    nlohmann::json j = original.ToJson();
    FlingConfig restored = FlingConfig::FromJson(j);

    EXPECT_EQ(restored.endpoint, original.endpoint);
    EXPECT_EQ(restored.region, original.region);
    EXPECT_EQ(restored.bucket, original.bucket);
    EXPECT_EQ(restored.accessKey, original.accessKey);
    EXPECT_EQ(restored.secretKey, original.secretKey);
    EXPECT_EQ(restored.expirySeconds, original.expirySeconds);
    EXPECT_EQ(restored.usePathStyle, original.usePathStyle);
}

TEST_F(FlingConfigTests, FromJson_MissingFields)
{
    nlohmann::json j = {{"endpoint", "http://test.com"}};

    FlingConfig config = FlingConfig::FromJson(j);

    EXPECT_EQ(config.endpoint, L"http://test.com");
    EXPECT_TRUE(config.region.empty());
    EXPECT_TRUE(config.bucket.empty());
    EXPECT_EQ(config.expirySeconds, 604800);  // Default
    EXPECT_FALSE(config.usePathStyle);  // Default
}

TEST_F(FlingConfigTests, FromJson_InvalidTypes)
{
    nlohmann::json j = {
        {"endpoint", 12345},       // Should be string
        {"expirySeconds", "abc"}   // Should be int
    };

    FlingConfig config = FlingConfig::FromJson(j);

    // Invalid types should be ignored
    EXPECT_TRUE(config.endpoint.empty());
    EXPECT_EQ(config.expirySeconds, 604800);  // Default
}

TEST_F(FlingConfigTests, WideToUtf8Conversion)
{
    std::wstring wide = L"Hello, World! \u4E2D\u6587";
    std::string utf8 = WideToUtf8(wide);
    std::wstring back = Utf8ToWide(utf8);

    EXPECT_EQ(wide, back);
}

TEST_F(FlingConfigTests, EmptyStringConversion)
{
    EXPECT_TRUE(WideToUtf8(L"").empty());
    EXPECT_TRUE(Utf8ToWide("").empty());
}
