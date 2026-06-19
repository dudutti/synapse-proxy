import nodemailer from "nodemailer";

import { prisma } from "./prisma";

export async function sendEmail({ 
  to, 
  subject, 
  html, 
  templateId, 
  variables = {} 
}: { 
  to: string; 
  subject?: string; 
  html?: string; 
  templateId?: string;
  variables?: Record<string, string>;
}) {
  let finalSubject = subject || "";
  let finalHtml = html || "";

  if (templateId) {
    const template = await prisma.emailTemplate.findUnique({
      where: { id: templateId }
    });

    if (template) {
      finalSubject = template.subject;
      finalHtml = template.html;

      // Replace variables in the template (e.g., {{URL}})
      for (const [key, value] of Object.entries(variables)) {
        const regex = new RegExp(`{{${key}}}`, "g");
        finalSubject = finalSubject.replace(regex, value);
        finalHtml = finalHtml.replace(regex, value);
      }
    } else {
      console.warn(`Email template ${templateId} not found, falling back to provided content`);
    }
  }

  // If SMTP_HOST is not set, we'll use a mocked "Ethereal" account for testing.
  let transporter;

  if (process.env.SMTP_HOST) {
    transporter = nodemailer.createTransport({
      host: process.env.SMTP_HOST,
      port: parseInt(process.env.SMTP_PORT || "465", 10),
      secure: process.env.SMTP_PORT === "465", // true for 465, false for other ports
      auth: {
        user: process.env.SMTP_USER,
        pass: process.env.SMTP_PASS,
      },
    });
  } else {
    // Development Mode (Ethereal Email)
    console.log("No SMTP settings found. Generating Ethereal test account...");
    const testAccount = await nodemailer.createTestAccount();
    transporter = nodemailer.createTransport({
      host: "smtp.ethereal.email",
      port: 587,
      secure: false,
      auth: {
        user: testAccount.user,
        pass: testAccount.pass,
      },
    });
  }

  const info = await transporter.sendMail({
    from: process.env.SMTP_FROM || '"Synapse Proxy" <noreply@synapse-proxy.com>',
    to,
    subject: finalSubject,
    html: finalHtml,
  });

  // Preview only available when sending through an Ethereal account
  if (!process.env.SMTP_HOST) {
    console.log("Preview URL: %s", nodemailer.getTestMessageUrl(info));
  }
}
