import type { Metadata } from "next";
import { Inter } from "next/font/google";
import "./globals.css";
import { AuthProvider } from "@/components/AuthProvider";
import { SwrProvider } from "@/components/SwrProvider";
import { Toaster } from 'sonner';
import Script from "next/script";
import { CookieBanner } from "@/components/CookieBanner";

const inter = Inter({ subsets: ["latin"] });

export const metadata: Metadata = {
  title: "Synapse Proxy Control Plane",
  description: "B2B SaaS LLM Proxy Dashboard",
};

export const dynamic = "force-dynamic";

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en">
      <head>
        <Script id="consent-mode" strategy="beforeInteractive" dangerouslySetInnerHTML={{
          __html: `
            window.dataLayer = window.dataLayer || [];
            function gtag(){dataLayer.push(arguments);}
            gtag('consent', 'default', {
              'ad_storage': 'denied',
              'analytics_storage': 'denied'
            });
          `
        }} />
        <Script id="gtm" strategy="afterInteractive" dangerouslySetInnerHTML={{
          __html: `
            (function(w,d,s,l,i){w[l]=w[l]||[];w[l].push({'gtm.start':
            new Date().getTime(),event:'gtm.js'});var f=d.getElementsByTagName(s)[0],
            j=d.createElement(s),dl=l!='dataLayer'?'&l='+l:'';j.async=true;j.src=
            'https://www.googletagmanager.com/gtm.js?id='+i+dl;f.parentNode.insertBefore(j,f);
            })(window,document,'script','dataLayer','GTM-KGQLM7WQ');
          `
        }} />
      </head>
      <body className={inter.className}>
        <noscript dangerouslySetInnerHTML={{
          __html: `<iframe src="https://www.googletagmanager.com/ns.html?id=GTM-KGQLM7WQ" height="0" width="0" style="display:none;visibility:hidden"></iframe>`
        }} />
        <SwrProvider>
          <AuthProvider>
            {children}
          </AuthProvider>
          <Toaster theme="dark" position="bottom-right" />
        </SwrProvider>
        <CookieBanner />
      </body>
    </html>
  );
}
