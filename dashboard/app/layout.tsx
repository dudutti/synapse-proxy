import type { Metadata } from "next";
import { Inter } from "next/font/google";
import "./globals.css";
import { AuthProvider } from "@/components/AuthProvider";
import { SwrProvider } from "@/components/SwrProvider";
import { Toaster } from 'sonner';

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
      <body className={inter.className}>
        <SwrProvider>
          <AuthProvider>
            {children}
          </AuthProvider>
          <Toaster theme="dark" position="bottom-right" />
        </SwrProvider>
      </body>
    </html>
  );
}
